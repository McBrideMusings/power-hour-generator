package tools

import "runtime"

// FilterRemediation returns platform-specific suggestions for fixing missing
// ffmpeg filters based on how ffmpeg was installed.
func FilterRemediation(missing []string, installMethod string) []string {
	if len(missing) == 0 {
		return nil
	}

	switch installMethod {
	case InstallMethodHomebrew:
		return []string{
			"brew reinstall ffmpeg",
		}
	case InstallMethodApt:
		return []string{
			"sudo apt install libavfilter-extra",
		}
	case InstallMethodSnap:
		return []string{
			"sudo snap refresh ffmpeg",
		}
	case InstallMethodManaged:
		return []string{
			"Download a full-featured build from https://www.ffmpeg.org/download.html",
			"or run: powerhour tools install ffmpeg --force",
		}
	default:
		return defaultFilterRemediation()
	}
}

func defaultFilterRemediation() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"Install a full-featured ffmpeg via Homebrew: brew install ffmpeg",
		}
	case "linux":
		return []string{
			"Install a full-featured ffmpeg, e.g.: sudo apt install ffmpeg libavfilter-extra",
			"or download a static build from https://www.ffmpeg.org/download.html",
		}
	default:
		return []string{
			"Install a full-featured ffmpeg from https://www.ffmpeg.org/download.html",
		}
	}
}
