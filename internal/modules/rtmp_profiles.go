package modules

import tg "github.com/amarnathcjd/gogram/telegram"

type PlatformProfile struct {
	Name            string
	Bitrate         string
	AudioBit        string
	FrameRate       int
	Resolution      string
	AudioSampleRate int
	LoopCount       int
}

var platformProfiles = map[string]PlatformProfile{
	"telegram": {
		Name:            "Telegram",
		Bitrate:         "8000k",
		AudioBit:        "128k",
		FrameRate:       60,
		Resolution:      "1920x1080",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"kick": {
		Name:            "Kick",
		Bitrate:         "8000k",
		AudioBit:        "128k",
		FrameRate:       60,
		Resolution:      "1920x1080",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"youtube": {
		Name:            "YouTube",
		Bitrate:         "6000k",
		AudioBit:        "128k",
		FrameRate:       30,
		Resolution:      "1920x1080",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"twitch": {
		Name:            "Twitch",
		Bitrate:         "6000k",
		AudioBit:        "160k",
		FrameRate:       60,
		Resolution:      "1920x1080",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"facebook": {
		Name:            "Facebook Live",
		Bitrate:         "4000k",
		AudioBit:        "128k",
		FrameRate:       30,
		Resolution:      "1280x720",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"trovo": {
		Name:            "Trovo",
		Bitrate:         "6000k",
		AudioBit:        "128k",
		FrameRate:       60,
		Resolution:      "1920x1080",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"dlive": {
		Name:            "DLive",
		Bitrate:         "4000k",
		AudioBit:        "128k",
		FrameRate:       30,
		Resolution:      "1280x720",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"tiktok": {
		Name:            "TikTok LIVE",
		Bitrate:         "3000k",
		AudioBit:        "128k",
		FrameRate:       30,
		Resolution:      "720x1280",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
	"custom": {
		Name:            "Custom RTMP",
		Bitrate:         "2000k",
		AudioBit:        "96k",
		FrameRate:       30,
		Resolution:      "1280x720",
		AudioSampleRate: 44100,
		LoopCount:       0,
	},
}

func applyProfile(stream *tg.RTMPStream, platform string) {
	profile, ok := platformProfiles[platform]
	if !ok {
		profile = platformProfiles["custom"]
	}
	stream.SetBitrate(profile.Bitrate)
	stream.SetAudioBitrate(profile.AudioBit)
	stream.SetFrameRate(profile.FrameRate)
	stream.SetLoopCount(profile.LoopCount)
}
