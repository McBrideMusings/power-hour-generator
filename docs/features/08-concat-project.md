08_concat_project.md

Objective
- Add optional concatenation stage to join all rendered segments into one "power hour" file.

Tasks
- Command: powerhour concat --out final.mp4
- Build concat.txt with all segment paths in order
- Invoke ffmpeg -f concat -safe 0 -i concat.txt -c copy out.mp4
- Optionally allow re-encode if formats mismatch (--reencode)

Acceptance
- Concatenated file plays through all segments
- Skips re-encode when segments are uniform