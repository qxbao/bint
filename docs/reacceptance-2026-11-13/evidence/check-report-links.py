from pathlib import Path
import re

root = Path("docs/reacceptance-2026-11-13")
files = [root / name for name in (
    "README.md", "core.md", "evolution.md", "tui.md", "isolation.md",
    "mcp.md", "experiments.md", "deferred.md",
)]
errors = []
checked = 0
for source in files:
    content = source.read_text()
    for match in re.finditer(r"\]\(([^)]+)\)", content):
        raw = match.group(1).split()[0]
        if raw.startswith(("http://", "https://", "mailto:")):
            continue
        target, _, fragment = raw.partition("#")
        destination = (source.parent / target).resolve() if target else source.resolve()
        line = content.count("\n", 0, match.start()) + 1
        checked += 1
        if not destination.exists():
            errors.append(f"{source}:{line}: missing file {raw}")
        if destination.exists() and fragment and destination.suffix == ".md":
            doc = destination.read_text()
            anchors = set(re.findall(r"<a\s+id=[\"']([^\"']+)", doc))
            for heading in re.findall(r"^#{1,6}\s+(.+)$", doc, re.M):
                # GitHub-style heading slug: remove inline code/markup and
                # punctuation, collapse whitespace, retain Unicode letters.
                heading = re.sub(r"[`*_~]", "", heading).strip().lower()
                anchors.add(re.sub(r"[^\w -]", "", heading).replace(" ", "-"))
            if fragment not in anchors:
                errors.append(f"{source}:{line}: missing anchor {raw}")

print(f"checked_links={checked} errors={len(errors)}")
for error in errors:
    print(error)
raise SystemExit(bool(errors))
