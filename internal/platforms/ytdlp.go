package platforms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	state "main/internal/core/models"
	"main/internal/utils"
)

const PlatformYtDlp state.PlatformName = "YtDlp"

type YtdlpPlatform struct {
	name state.PlatformName
}

type ytdlpInfo struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Duration    float64     `json:"duration"`
	Thumbnail   string      `json:"thumbnail"`
	URL         string      `json:"webpage_url"`
	OriginalURL string      `json:"original_url"`
	Uploader    string      `json:"uploader"`
	Description string      `json:"description"`
	IsLive      bool        `json:"is_live"`
	Extractor   string      `json:"extractor"`
	Entries     []ytdlpInfo `json:"entries"`
}

type ytdlpCapabilities struct {
	workDir      string
	cookiesPath  string
	hasCookies   bool
	cookiesFresh bool
	hasDeno      bool
	hasNode      bool
	hasFFmpeg    bool
	runtimeNames []string
}

type ytdlpStrategy struct {
	name             string
	format           string
	audioFormat      string
	useCookies       bool
	useImpersonation bool
	useExtractorArgs bool
	useRemoteEJS     bool
	useNoJSRuntimes  bool
	runtimeName      string
	userAgent        string
	concurrentParts  int
}

type ytdlpErrorCode string

const (
	errYtDlpLoginRequired  ytdlpErrorCode = "youtube_login_required"
	errYtDlpBotChallenge   ytdlpErrorCode = "youtube_bot_challenge"
	errYtDlpRateLimited    ytdlpErrorCode = "youtube_rate_limited"
	errYtDlpPrivateContent ytdlpErrorCode = "private_content"
	errYtDlpUnavailable    ytdlpErrorCode = "content_unavailable"
	errYtDlpGeoRestricted  ytdlpErrorCode = "geo_restricted"
	errYtDlpUnsupportedURL ytdlpErrorCode = "unsupported_url"
	errYtDlpFormatMissing  ytdlpErrorCode = "format_unavailable"
	errYtDlpFFmpegMissing  ytdlpErrorCode = "ffmpeg_missing"
	errYtDlpRuntimeMissing ytdlpErrorCode = "js_runtime_missing"
	errYtDlpNetwork        ytdlpErrorCode = "transient_network"
	errYtDlpExtractor      ytdlpErrorCode = "extractor_failure"
)

type ytdlpAttemptError struct {
	Code   ytdlpErrorCode
	Reason string
	Cause  error
	Raw    string
}

func (e *ytdlpAttemptError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return e.Reason
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "yt-dlp error"
}

func (e *ytdlpAttemptError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

var (
	ytdlpCapsOnce sync.Once
	ytdlpCaps     ytdlpCapabilities

	ytdlpMetadataCache         = utils.NewCache[string, *ytdlpInfo](15 * time.Minute)
	ytdlpPreferredStrategy     = utils.NewCache[string, string](20 * time.Minute)
	ytdlpStrategyCooldownUntil = utils.NewCache[string, time.Time](10 * time.Minute)
)

const ytdlpCookieFreshnessWindow = 45 * time.Minute

var bannedExtractors = map[string]bool{
	"alphaporno":     true,
	"beeg":           true,
	"behindkink":     true,
	"bongacams":      true,
	"cam4":           true,
	"cammodels":      true,
	"camsoda":        true,
	"chaturbate":     true,
	"drtuber":        true,
	"eporner":        true,
	"erocast":        true,
	"eroprofile":     true,
	"fourtube":       true,
	"goshgay":        true,
	"hellporno":      true,
	"iwara":          true,
	"lovehomeporn":   true,
	"manyvids":       true,
	"motherless":     true,
	"murrtube":       true,
	"nonktube":       true,
	"noodlemagazine": true,
	"nubilesporn":    true,
	"nuvid":          true,
	"oftv":           true,
	"peekvids":       true,
	"pornbox":        true,
	"pornflip":       true,
	"pornhub":        true,
	"pornotube":      true,
	"pornovoisines":  true,
	"pornoxo":        true,
	"redgifs":        true,
	"redtube":        true,
	"rule34video":    true,
	"sauceplus":      true,
	"sexu":           true,
	"slutload":       true,
	"spankbang":      true,
	"stripchat":      true,
	"sunporno":       true,
	"thisvid":        true,
	"tnaflix":        true,
	"toypics":        true,
	"txxx":           true,
	"xhamster":       true,
	"xnxx":           true,
	"xvideos":        true,
	"xxxymovies":     true,
	"youjizz":        true,
	"youporn":        true,
	"zenporn":        true,
}

// URLs that are likely handled by YouTube
var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(youtube\.com|youtu\.be|music\.youtube\.com)`),
}

func init() {
	Register(60, &YtdlpPlatform{
		name: PlatformYtDlp,
	})
}

func (y *YtdlpPlatform) Name() state.PlatformName {
	return y.name
}

// CanGetTracks checks if this is a valid URL that yt-dlp might handle
func (y *YtdlpPlatform) CanGetTracks(query string) bool {
	query = strings.TrimSpace(query)
	if _, err := sanitizeMediaURL(query); err != nil {
		return false
	}

	// Must be a URL
	parsedURL, err := url.Parse(query)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}

	host := strings.ToLower(parsedURL.Host)

	// Ignore Telegram URLs ( already handled by TeleramPlatform)
	if host == "t.me" ||
		host == "telegram.me" ||
		host == "telegram.dog" ||
		strings.HasSuffix(host, ".t.me") {
		return false
	}

	return true
}

// GetTracks extracts metadata using yt-dlp
func (y *YtdlpPlatform) GetTracks(
	query string,
	video bool,
) ([]*state.Track, error) {
	query = strings.TrimSpace(query)
	safeURL, err := sanitizeMediaURL(query)
	if err != nil {
		return nil, errUnsafeURL
	}

	gologging.InfoF("YtDlp: Extracting metadata for %s", query)

	info, err := y.extractMetadata(safeURL)
	if err != nil {
		gologging.ErrorF("YtDlp: Failed to extract metadata: %v", err)
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	// Check if it's a live stream
	if info.IsLive {
		gologging.Info("YtDlp: Detected live stream, returning error")
		return nil, errors.New(
			"live streams are not supported by yt-dlp downloader",
		)
	}

	// Check for banned extractor
	if bannedExtractors[strings.ToLower(info.Extractor)] {
		gologging.InfoF("YtDlp: Blocked adult content from extractor: %s", info.Extractor)
		return nil, errors.New("adult content is not allowed")
	}

	var tracks []*state.Track

	// Handle playlists
	if len(info.Entries) > 0 {
		gologging.InfoF(
			"YtDlp: Found playlist with %d entries",
			len(info.Entries),
		)
		for _, entry := range info.Entries {
			if entry.IsLive {
				continue // Skip live entries
			}
			// Check entry extractor if present (sometimes entries have their own extractor info)
			if entry.Extractor != "" &&
				bannedExtractors[strings.ToLower(entry.Extractor)] {
				gologging.InfoF(
					"YtDlp: Skipping banned entry from extractor: %s",
					entry.Extractor,
				)
				continue
			}
			track := y.infoToTrack(&entry, video)
			tracks = append(tracks, track)
		}
	} else {
		track := y.infoToTrack(info, video)
		tracks = []*state.Track{track}
	}

	if len(tracks) > 0 {
		gologging.InfoF(
			"YtDlp: Successfully extracted %d track(s)",
			len(tracks),
		)
	}

	return tracks, nil
}

func (y *YtdlpPlatform) CanDownload(source state.PlatformName) bool {
	return source == y.name || source == PlatformYouTube
}

func getYtDlpCapabilities() ytdlpCapabilities {
	ytdlpCapsOnce.Do(func() {
		caps := ytdlpCapabilities{
			workDir: resolveYtDlpWorkDir(),
		}

		caps.cookiesPath, caps.hasCookies = resolveCookiesPath(caps.workDir)
		caps.cookiesFresh = isFreshCookieFile(caps.cookiesPath, ytdlpCookieFreshnessWindow)
		caps.hasFFmpeg = commandExists("ffmpeg")
		caps.hasDeno = commandExists("deno")
		caps.hasNode = commandExists("node")

		switch {
		case caps.hasDeno:
			caps.runtimeNames = append(caps.runtimeNames, "deno")
		case caps.hasNode:
			caps.runtimeNames = append(caps.runtimeNames, "node")
		}

		ytdlpCaps = caps
		gologging.InfoF(
			"YtDlp: capabilities workdir=%s cookies=%t cookiesFresh=%t ffmpeg=%t runtimes=%v os=%s",
			caps.workDir,
			caps.hasCookies,
			caps.cookiesFresh,
			caps.hasFFmpeg,
			caps.runtimeNames,
			runtime.GOOS,
		)
	})

	return ytdlpCaps
}

func resolveYtDlpWorkDir() string {
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return wd
	}
	return "."
}

func resolveCookiesPath(workDir string) (string, bool) {
	candidates := []string{
		filepath.Join(workDir, "cookies.txt"),
		"cookies.txt",
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		abs := candidate
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workDir, candidate)
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, true
		}
	}

	return filepath.Join(workDir, "cookies.txt"), false
}

func isFreshCookieFile(path string, freshWindow time.Duration) bool {
	if path == "" || freshWindow <= 0 {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return time.Since(info.ModTime()) <= freshWindow
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func quoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\r\"") {
			quoted = append(quoted, fmt.Sprintf("%q", arg))
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

func (y *YtdlpPlatform) appendYouTubeAuthArgs(
	args []string,
	strat ytdlpStrategy,
	caps ytdlpCapabilities,
) []string {
	if strat.useCookies {
		if caps.hasCookies {
			args = append(args, "--cookies", caps.cookiesPath)
		} else {
			gologging.WarnF(
				"YtDlp: cookies requested by strategy %s but file not found at %s",
				strat.name,
				caps.cookiesPath,
			)
		}
	}

	if strat.useImpersonation {
		args = append(args, "--impersonate", "chrome")
	}

	if strat.useExtractorArgs {
		args = append(args, "--extractor-args", "youtube:player_client=ios,android,web")
	}

	if strat.useRemoteEJS {
		args = append(args, "--remote-components", "ejs:github")
	}

	if strat.useNoJSRuntimes {
		args = append(args, "--no-js-runtimes")
	} else if strat.runtimeName != "" {
		args = append(args, "--js-runtimes", strat.runtimeName)
	}

	if strat.userAgent != "" {
		args = append(args, "--user-agent", strat.userAgent)
	}

	return args
}

func defaultChromeUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"
}

func preferredStrategyCacheKey(kind string, isYouTube bool, video bool) string {
	return fmt.Sprintf("%s:yt=%t:video=%t", kind, isYouTube, video)
}

func strategyCooldownKey(kind string, strategyName string) string {
	return kind + ":" + strategyName
}

func preferAndFilterStrategies(kind string, strategies []ytdlpStrategy, preferredKey string) []ytdlpStrategy {
	if len(strategies) <= 1 {
		return strategies
	}
	filtered := make([]ytdlpStrategy, 0, len(strategies))
	now := time.Now()
	for _, strat := range strategies {
		if until, ok := ytdlpStrategyCooldownUntil.Get(strategyCooldownKey(kind, strat.name)); ok && until.After(now) {
			continue
		}
		filtered = append(filtered, strat)
	}
	if len(filtered) == 0 {
		filtered = append(filtered, strategies...)
	}
	preferred, ok := ytdlpPreferredStrategy.Get(preferredKey)
	if !ok || preferred == "" {
		return filtered
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].name == preferred {
			return true
		}
		if filtered[j].name == preferred {
			return false
		}
		return false
	})
	return filtered
}

func markStrategySuccess(kind string, preferredKey string, strat ytdlpStrategy) {
	ytdlpPreferredStrategy.Set(preferredKey, strat.name)
	ytdlpStrategyCooldownUntil.Delete(strategyCooldownKey(kind, strat.name))
}

func markStrategyCooldown(kind string, strat ytdlpStrategy, attemptErr *ytdlpAttemptError) {
	if attemptErr == nil {
		return
	}
	var cooldown time.Duration
	switch attemptErr.Code {
	case errYtDlpLoginRequired, errYtDlpBotChallenge, errYtDlpRateLimited:
		cooldown = 12 * time.Minute
	case errYtDlpFormatMissing, errYtDlpRuntimeMissing:
		cooldown = 5 * time.Minute
	default:
		return
	}
	ytdlpStrategyCooldownUntil.Set(strategyCooldownKey(kind, strat.name), time.Now().Add(cooldown))
}

func isRetryableYtDlpCode(code ytdlpErrorCode) bool {
	switch code {
	case errYtDlpNetwork, errYtDlpFormatMissing, errYtDlpRuntimeMissing:
		return true
	case errYtDlpLoginRequired, errYtDlpBotChallenge, errYtDlpRateLimited:
		return true
	case errYtDlpUnavailable, errYtDlpGeoRestricted:
		return true
	default:
		return false
	}
}

func shouldEscalateYouTubeStrategy(err *ytdlpAttemptError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case errYtDlpLoginRequired, errYtDlpBotChallenge, errYtDlpRateLimited:
		return true
	default:
		return false
	}
}

func classifyYtDlpError(stderr string, runErr error) error {
	trimmed := strings.TrimSpace(stderr)
	lower := strings.ToLower(trimmed)

	wrap := func(code ytdlpErrorCode, reason string) error {
		return &ytdlpAttemptError{
			Code:   code,
			Reason: reason,
			Cause:  runErr,
			Raw:    trimmed,
		}
	}

	switch {
	case strings.Contains(lower, "sign in to confirm you're not a bot") ||
		strings.Contains(lower, "sign in to confirm you’re not a bot"):
		return wrap(errYtDlpBotChallenge, "YouTube requires a refreshed logged-in session for this video")
	case strings.Contains(lower, "login_required"):
		return wrap(errYtDlpLoginRequired, "YouTube requires authentication for this content")
	case strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "http error 429") ||
		strings.Contains(lower, "this content isn't available, try again later"):
		return wrap(errYtDlpRateLimited, "YouTube is rate-limiting requests right now")
	case strings.Contains(lower, "http error 403") ||
		strings.Contains(lower, "forbidden"):
		return wrap(errYtDlpRateLimited, "YouTube rejected this request with HTTP 403")
	case strings.Contains(lower, "private video") || strings.Contains(lower, "members-only"):
		return wrap(errYtDlpPrivateContent, "This content requires a different account or membership")
	case strings.Contains(lower, "video unavailable") ||
		strings.Contains(lower, "this video is unavailable") ||
		strings.Contains(lower, "content isn't available"):
		return wrap(errYtDlpUnavailable, "This content is not currently available")
	case strings.Contains(lower, "not available from your location") ||
		strings.Contains(lower, "geo") && strings.Contains(lower, "restricted"):
		return wrap(errYtDlpGeoRestricted, "This content is geo-restricted")
	case strings.Contains(lower, "unsupported url"):
		return wrap(errYtDlpUnsupportedURL, "This URL is not supported by yt-dlp")
	case strings.Contains(lower, "requested format is not available") ||
		strings.Contains(lower, "no video formats found"):
		return wrap(errYtDlpFormatMissing, "No compatible format was available for this download attempt")
	case strings.Contains(lower, "ffmpeg is not installed") ||
		strings.Contains(lower, "ffprobe and ffmpeg not found"):
		return wrap(errYtDlpFFmpegMissing, "ffmpeg is required for the selected download format")
	case strings.Contains(lower, "javascript runtime") ||
		strings.Contains(lower, "js challenge providers") && strings.Contains(lower, "unavailable"):
		return wrap(errYtDlpRuntimeMissing, "No supported JavaScript runtime is available for this attempt")
	case strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "temporary failure") ||
		strings.Contains(lower, "connection reset"):
		return wrap(errYtDlpNetwork, "The upstream service temporarily failed while downloading")
	case strings.Contains(lower, "unable to extract") ||
		strings.Contains(lower, "extractorerror"):
		return wrap(errYtDlpExtractor, "yt-dlp extractor could not parse this content")
	case trimmed != "":
		return wrap(errYtDlpExtractor, trimmed)
	default:
		return &ytdlpAttemptError{
			Code:   errYtDlpExtractor,
			Reason: "yt-dlp download failed",
			Cause:  runErr,
		}
	}
}

func userFacingYtDlpError(err error, caps ytdlpCapabilities) error {
	attemptErr := extractAttemptError(err)
	if attemptErr == nil {
		return err
	}

	switch attemptErr.Code {
	case errYtDlpBotChallenge, errYtDlpLoginRequired:
		if caps.hasCookies && !caps.cookiesFresh {
			return errors.New("YouTube session expired. Refresh cookies.txt and try again")
		}
		if caps.hasCookies {
			return errors.New("YouTube requires a valid logged-in session. Refresh cookies.txt and try again")
		}
		return errors.New("YouTube blocked the request and needs a valid session or fresher access mode")
	case errYtDlpRateLimited:
		if caps.hasCookies && !caps.cookiesFresh {
			return errors.New("YouTube rejected the current session. Refresh cookies.txt or wait a bit and try again")
		}
		return errors.New("YouTube is rate-limiting or rejecting this request right now")
	case errYtDlpPrivateContent:
		return errors.New("This content needs a different account, membership, or private access")
	case errYtDlpUnavailable:
		return errors.New("This content is not currently available")
	case errYtDlpGeoRestricted:
		return errors.New("This content is geo-restricted on the current server")
	case errYtDlpFFmpegMissing:
		return errors.New("ffmpeg is missing for the selected media format")
	case errYtDlpRuntimeMissing:
		return errors.New("A supported JavaScript runtime is missing for this download")
	case errYtDlpFormatMissing:
		return errors.New("No compatible media format was available for this server")
	case errYtDlpUnsupportedURL:
		return errors.New("This URL is not supported for yt-dlp playback")
	default:
		return err
	}
}

func buildMetadataStrategies(isYouTube bool, caps ytdlpCapabilities) []ytdlpStrategy {
	strategies := []ytdlpStrategy{{name: "default-metadata"}}
	if !isYouTube {
		return strategies
	}

	variants := youtubeStrategyVariants(
		ytdlpStrategy{name: "youtube-metadata"},
		caps,
		true,
	)
	return preferAndFilterStrategies(
		"metadata",
		variants,
		preferredStrategyCacheKey("metadata", true, false),
	)
}

func buildDownloadStrategies(track *state.Track, isYouTube bool, caps ytdlpCapabilities) []ytdlpStrategy {
	if track.Video {
		return buildVideoDownloadStrategies(isYouTube, caps)
	}
	return buildAudioDownloadStrategies(isYouTube, caps)
}

func buildAudioDownloadStrategies(isYouTube bool, caps ytdlpCapabilities) []ytdlpStrategy {
	formats := []ytdlpStrategy{
		{
			name:        "audio-best",
			format:      "bestaudio[ext=m4a]/bestaudio/best",
			audioFormat: chooseAudioFormat(caps),
		},
		{
			name:        "audio-relaxed",
			format:      "ba/bestaudio/best",
			audioFormat: chooseAudioFormat(caps),
		},
	}

	if !isYouTube {
		return formats
	}

	var out []ytdlpStrategy
	for _, authVariant := range youtubeStrategyVariants(ytdlpStrategy{}, caps, false) {
		for _, formatVariant := range formats {
			combined := formatVariant
			combined.name = formatVariant.name + "-" + authVariant.name
			combined.useCookies = authVariant.useCookies
			combined.useImpersonation = authVariant.useImpersonation
			combined.useExtractorArgs = authVariant.useExtractorArgs
			combined.useRemoteEJS = authVariant.useRemoteEJS
			combined.useNoJSRuntimes = authVariant.useNoJSRuntimes
			combined.runtimeName = authVariant.runtimeName
			combined.userAgent = authVariant.userAgent
			out = append(out, combined)
		}
	}

	return preferAndFilterStrategies(
		"download",
		out,
		preferredStrategyCacheKey("download", true, false),
	)
}

func buildVideoDownloadStrategies(isYouTube bool, caps ytdlpCapabilities) []ytdlpStrategy {
	formats := []ytdlpStrategy{
		{
			name:   "video-combined-18",
			format: "18/b[height<=360]/best[height<=360]/best",
		},
		{
			name:   "video-combined-1080",
			format: "best[height<=1080]/best",
		},
	}
	if caps.hasFFmpeg {
		formats = append(formats,
			ytdlpStrategy{
				name:   "video-merged-1080",
				format: "bestvideo*[height<=1080]+bestaudio/best[height<=1080]/best",
			},
		)
	}

	if !isYouTube {
		return formats
	}

	var out []ytdlpStrategy
	for _, authVariant := range youtubeStrategyVariants(ytdlpStrategy{}, caps, false) {
		for _, formatVariant := range formats {
			combined := formatVariant
			combined.name = formatVariant.name + "-" + authVariant.name
			combined.useCookies = authVariant.useCookies
			combined.useImpersonation = authVariant.useImpersonation
			combined.useExtractorArgs = authVariant.useExtractorArgs
			combined.useRemoteEJS = authVariant.useRemoteEJS
			combined.useNoJSRuntimes = authVariant.useNoJSRuntimes
			combined.runtimeName = authVariant.runtimeName
			combined.userAgent = authVariant.userAgent
			out = append(out, combined)
		}
	}

	return preferAndFilterStrategies(
		"download",
		out,
		preferredStrategyCacheKey("download", true, true),
	)
}

func youtubeStrategyVariants(
	base ytdlpStrategy,
	caps ytdlpCapabilities,
	includeNoJS bool,
) []ytdlpStrategy {
	runtimeName := firstRuntimeName(caps)
	variants := []ytdlpStrategy{
		mergeStrategy(base, ytdlpStrategy{name: "guest"}),
		mergeStrategy(base, ytdlpStrategy{
			name:             "guest-browser",
			useImpersonation: true,
			useExtractorArgs: true,
			useRemoteEJS:     runtimeName != "",
			runtimeName:      runtimeName,
			userAgent:        defaultChromeUserAgent(),
		}),
	}

	if caps.hasCookies {
		variants = append(variants,
			mergeStrategy(base, ytdlpStrategy{name: "cookies", useCookies: true}),
			mergeStrategy(base, ytdlpStrategy{
				name:             "cookies-browser",
				useCookies:       true,
				useImpersonation: true,
				useExtractorArgs: true,
				useRemoteEJS:     runtimeName != "",
				runtimeName:      runtimeName,
				userAgent:        defaultChromeUserAgent(),
			}),
		)
		if includeNoJS {
			variants = append(variants,
				mergeStrategy(base, ytdlpStrategy{
					name:             "cookies-no-js",
					useCookies:       true,
					useImpersonation: true,
					userAgent:        defaultChromeUserAgent(),
					useNoJSRuntimes:  true,
				}),
			)
		}
	}

	return variants
}

func mergeStrategy(base, override ytdlpStrategy) ytdlpStrategy {
	merged := base
	if override.name != "" {
		if merged.name != "" {
			merged.name += "-" + override.name
		} else {
			merged.name = override.name
		}
	}
	if override.format != "" {
		merged.format = override.format
	}
	if override.audioFormat != "" {
		merged.audioFormat = override.audioFormat
	}
	merged.useCookies = override.useCookies
	merged.useImpersonation = override.useImpersonation
	merged.useExtractorArgs = override.useExtractorArgs
	merged.useRemoteEJS = override.useRemoteEJS
	merged.useNoJSRuntimes = override.useNoJSRuntimes
	if override.runtimeName != "" {
		merged.runtimeName = override.runtimeName
	}
	if override.userAgent != "" {
		merged.userAgent = override.userAgent
	}
	if override.concurrentParts > 0 {
		merged.concurrentParts = override.concurrentParts
	}
	return merged
}

func chooseAudioFormat(caps ytdlpCapabilities) string {
	if caps.hasFFmpeg {
		return "mp3"
	}
	return ""
}

func firstRuntimeName(caps ytdlpCapabilities) string {
	if len(caps.runtimeNames) == 0 {
		return ""
	}
	return caps.runtimeNames[0]
}

func (y *YtdlpPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {
	if f := findFile(track); f != "" {
		gologging.Debug("Ytdlp: Download -> Cached File -> " + f)
		return f, nil
	}

	gologging.InfoF("YtDlp: Downloading %s", track.Title)
	caps := getYtDlpCapabilities()
	isYouTube := y.isYouTubeURL(track.URL)
	strategies := buildDownloadStrategies(track, isYouTube, caps)
	preferredKey := preferredStrategyCacheKey("download", isYouTube, track.Video)

	var lastErr error
	for _, strat := range strategies {
		path, err := y.downloadWithStrategy(ctx, track, strat, caps, isYouTube)
		if err == nil {
			markStrategySuccess("download", preferredKey, strat)
			gologging.InfoF("YtDlp: Successfully downloaded %s via strategy %s", path, strat.name)
			return path, nil
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		lastErr = err
		attemptErr := extractAttemptError(err)
		markStrategyCooldown("download", strat, attemptErr)
		if isYouTube && shouldEscalateYouTubeStrategy(attemptErr) {
			gologging.WarnF("YtDlp: escalating YouTube strategy after %s failed: %v", strat.name, err)
			continue
		}
		if attemptErr != nil && isRetryableYtDlpCode(attemptErr.Code) {
			gologging.WarnF("YtDlp: retryable strategy failure %s: %v", strat.name, err)
			continue
		}
		break
	}

	if lastErr != nil {
		return "", userFacingYtDlpError(lastErr, caps)
	}

	return "", errors.New("yt-dlp download failed without a classified error")
}

func (y *YtdlpPlatform) downloadWithStrategy(
	ctx context.Context,
	track *state.Track,
	strat ytdlpStrategy,
	caps ytdlpCapabilities,
	isYouTube bool,
) (string, error) {
	safeURL, err := sanitizeMediaURL(track.URL)
	if err != nil {
		return "", errUnsafeURL
	}

	args := []string{
		"--no-playlist",
		"--no-part",
		"--geo-bypass",
		"--no-warnings",
		"--ignore-errors",
		"--no-check-certificate",
		"-q",
		"-o", getPath(track, ".%(ext)s"),
	}

	if strat.concurrentParts > 0 {
		args = append(args, "--concurrent-fragments", fmt.Sprintf("%d", strat.concurrentParts))
	} else {
		args = append(args, "--concurrent-fragments", "4")
	}

	if strat.format != "" {
		args = append(args, "-f", strat.format)
	}

	if !track.Video {
		if strat.audioFormat != "" && caps.hasFFmpeg {
			args = append(args, "-x", "--audio-format", strat.audioFormat)
		}
	}

	if isYouTube {
		args = y.appendYouTubeAuthArgs(args, strat, caps)
	}

	args = append(args, "--", safeURL)

	gologging.InfoF(
		"YtDlp: Running download strategy %s: yt-dlp %s",
		strat.name,
		quoteArgs(args),
	)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Dir = caps.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := strings.TrimSpace(stderr.String())
		outStr := strings.TrimSpace(stdout.String())
		gologging.ErrorF(
			"YtDlp: Download strategy %s failed for %s. System Err: %v\n--- RAW YT-DLP STDERR ---\n%s\n--- RAW YT-DLP STDOUT ---\n%s\n-----------------------",
			strat.name,
			track.URL,
			err,
			errStr,
			outStr,
		)
		findAndRemove(track)

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		return "", classifyYtDlpError(joinStdStreams(errStr, outStr), err)
	}

	path := findFile(track)
	if path == "" {
		return "", &ytdlpAttemptError{
			Code:   errYtDlpExtractor,
			Reason: "yt-dlp finished without producing an output file",
		}
	}

	return path, nil
}

// extractMetadata uses yt-dlp to extract video/audio metadata
func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {
	safeURL, err := sanitizeMediaURL(urlStr)
	if err != nil {
		return nil, errUnsafeURL
	}
	cacheKey := "metadata:" + strings.ToLower(safeURL)
	if cached, ok := ytdlpMetadataCache.Get(cacheKey); ok && cached != nil {
		return cached, nil
	}
	caps := getYtDlpCapabilities()
	isYouTube := y.isYouTubeURL(urlStr)
	strategies := buildMetadataStrategies(isYouTube, caps)
	preferredKey := preferredStrategyCacheKey("metadata", isYouTube, false)

	var output string
	var lastErr error
	var successStrategy ytdlpStrategy
	for _, strat := range strategies {
		result, err := y.extractMetadataWithStrategy(safeURL, strat, caps, isYouTube)
		if err == nil {
			output = result
			lastErr = nil
			successStrategy = strat
			break
		}
		lastErr = err
		attemptErr := extractAttemptError(err)
		markStrategyCooldown("metadata", strat, attemptErr)
		if isYouTube && shouldEscalateYouTubeStrategy(attemptErr) {
			continue
		}
		if attemptErr != nil && isRetryableYtDlpCode(attemptErr.Code) {
			continue
		}
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("metadata extraction failed: %w", lastErr)
	}
	markStrategySuccess("metadata", preferredKey, successStrategy)

	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Handle playlists (multiple JSON objects)
	if len(lines) > 1 {
		var info ytdlpInfo
		info.Entries = make([]ytdlpInfo, 0, len(lines))

		for _, line := range lines {
			var entry ytdlpInfo
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				gologging.ErrorF("YtDlp: Failed to parse entry JSON: %v", err)
				continue
			}
			info.Entries = append(info.Entries, entry)
		}

		if len(info.Entries) == 0 {
			return nil, errors.New("no valid entries found in playlist")
		}

		ytdlpMetadataCache.Set(cacheKey, &info)
		return &info, nil
	}

	// Single video/audio
	var info ytdlpInfo
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		gologging.ErrorF("YtDlp: Failed to parse JSON: %v", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	ytdlpMetadataCache.Set(cacheKey, &info)
	return &info, nil
}

func (y *YtdlpPlatform) extractMetadataWithStrategy(
	safeURL string,
	strat ytdlpStrategy,
	caps ytdlpCapabilities,
	isYouTube bool,
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{
		"-j",
		"--flat-playlist",
		"--no-warnings",
		"--no-check-certificate",
	}

	if isYouTube {
		args = y.appendYouTubeAuthArgs(args, strat, caps)
	}

	args = append(args, "--", safeURL)

	gologging.InfoF(
		"YtDlp: Running metadata strategy %s: yt-dlp %s",
		strat.name,
		quoteArgs(args),
	)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Dir = caps.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := strings.TrimSpace(stderr.String())
		outStr := strings.TrimSpace(stdout.String())
		gologging.ErrorF(
			"YtDlp: Metadata strategy %s failed. System Err: %v\n--- RAW YT-DLP STDERR ---\n%s\n--- RAW YT-DLP STDOUT ---\n%s\n-----------------------",
			strat.name,
			err,
			errStr,
			outStr,
		)
		return "", classifyYtDlpError(joinStdStreams(errStr, outStr), err)
	}

	return stdout.String(), nil
}

func joinStdStreams(stderr, stdout string) string {
	stderr = strings.TrimSpace(stderr)
	stdout = strings.TrimSpace(stdout)
	switch {
	case stderr != "" && stdout != "":
		return stderr + "\n" + stdout
	case stderr != "":
		return stderr
	default:
		return stdout
	}
}

func extractAttemptError(err error) *ytdlpAttemptError {
	var attemptErr *ytdlpAttemptError
	if errors.As(err, &attemptErr) {
		return attemptErr
	}
	return nil
}

// infoToTrack converts yt-dlp info to Track
func (y *YtdlpPlatform) infoToTrack(
	info *ytdlpInfo,
	video bool,
) *state.Track {
	duration := int(info.Duration)

	trackURL := info.URL
	if info.OriginalURL != "" {
		trackURL = info.OriginalURL
	}

	return &state.Track{
		ID:       info.ID,
		Title:    info.Title,
		Duration: duration,
		Artwork:  info.Thumbnail,
		URL:      trackURL,
		Source:   PlatformYtDlp,
		Video:    video,
	}
}

// isYouTubeURL checks if the URL is from YouTube
func (y *YtdlpPlatform) isYouTubeURL(urlStr string) bool {
	for _, pattern := range youtubePatterns {
		if pattern.MatchString(urlStr) {
			return true
		}
	}
	return false
}
