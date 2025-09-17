09_project_lifecycle.md

Objective
- Add commands to manage full project lifecycle.

Tasks
- powerhour clean: remove segments/ or logs/ selectively
- powerhour status: print summary (clips count, cached sources, rendered segments, final file status)
- powerhour doctor: check for common issues (missing tools, config errors, broken cache)
- powerhour export plan.json: export normalized plan for external tooling

Acceptance
- Users can reset segments without deleting src/
- status shows concise project health
- doctor detects missing dependencies