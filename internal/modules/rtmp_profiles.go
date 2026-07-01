package modules

import tg "github.com/amarnathcjd/gogram/telegram"

type PlatformProfile struct {
	Name      string
	Bitrate   string
	AudioBit  string
	FrameRate int
	LoopCount int
}

var platformProfiles = map[string]PlatformProfile{
	"kick": {
		Name:      "Kick",
		Bitrate:   "8000k",
		AudioBit:  "128k",
		FrameRate: 60,
		LoopCount: 0,
	},
	"youtube": {
		Name:      "YouTube",
		Bitrate:   "6000k",
		AudioBit:  "128k",
		FrameRate: 30,
		LoopCount: 0,
	},
	"twitch": {
		Name:      "Twitch",
		Bitrate:   "6000k",
		AudioBit:  "128k",
		FrameRate: 30,
		LoopCount: 0,
	},
	"custom": {
		Name:      "Custom",
		Bitrate:   "2000k",
		AudioBit:  "96k",
		FrameRate: 30,
		LoopCount: 0,
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
