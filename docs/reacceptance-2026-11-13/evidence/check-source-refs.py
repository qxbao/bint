from pathlib import Path
import re

reports = [Path("docs/reacceptance-2026-11-13") / name for name in (
    "README.md", "core.md", "evolution.md", "tui.md", "isolation.md",
    "mcp.md", "experiments.md", "deferred.md",
)]
pattern = re.compile(
    r"(?<!\w)((?:docs|internal|cmd)/[\w./-]+?\.(?:md|go|sh|yml|yaml|sql|toml)):(\d+)(?:-(\d+))?"
)
errors = []
checked = 0
for report in reports:
    for report_line, content in enumerate(report.read_text().splitlines(), 1):
        for match in pattern.finditer(content):
            source = Path(match.group(1))
            first = int(match.group(2))
            last = int(match.group(3) or first)
            checked += 1
            if not source.exists():
                errors.append(f"{report}:{report_line}: missing source {source}")
                continue
            line_count = sum(1 for _ in source.open())
            if first > line_count or last > line_count:
                errors.append(
                    f"{report}:{report_line}: out-of-range {source}:{first}-{last} (lines={line_count})"
                )
print(f"checked_source_refs={checked} errors={len(errors)}")
for error in errors:
    print(error)
raise SystemExit(bool(errors))
