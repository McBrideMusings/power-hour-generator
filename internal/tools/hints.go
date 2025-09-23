package tools

import "runtime"

func installHints(tool string) []string {
	if tool != "ffmpeg" {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		return []string{
			"Install ffmpeg via Homebrew: brew install ffmpeg",
		}
	case "linux":
		return []string{
			"Install ffmpeg with your distro package manager, e.g. sudo apt install ffmpeg",
		}
	case "windows":
		return []string{
			"Install ffmpeg via winget: winget install Gyan.FFmpeg",
			"or via Chocolatey: choco install ffmpeg",
		}
	default:
		return []string{"Install ffmpeg using your platform's package manager"}
	}
}
