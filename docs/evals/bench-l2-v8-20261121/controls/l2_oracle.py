"""One-off state migration for the 48dbfad control binary."""
import hashlib
import json
from contextlib import closing
from pathlib import Path
import re
import sqlite3


def quote(identifier):
    return '"' + identifier.replace('"', '""') + '"'


def key(repo):
    common = repo.resolve() / ".git"
    slug = re.sub(r"[^A-Za-z0-9._-]+", "-", repo.name).strip("-.")[:40] or "repo"
    return f'{slug}-{hashlib.sha256(common.as_posix().lower().encode()).hexdigest()[:12]}'


def migrate(fixture, old_repo):
    roots = [p.parent for p in fixture.state.glob("*/repo.json")]
    if len(roots) != 1:
        raise RuntimeError(f"oracle expected a single old state directory, found {len(roots)}")
    old_state = roots[0]
    new_repo = fixture.repo
    new_state = fixture.state / key(new_repo)
    if str(old_state) != str(new_state):
        old_state.rename(new_state)

    paths = ((old_state, new_state), (old_repo, new_repo))
    pairs = [(str(old), str(new)) for old, new in paths]
    pairs.extend((old.as_posix(), new.as_posix()) for old, new in paths)
    pairs.append((old_state.name, new_state.name))

    def update(value):
        if isinstance(value, dict):
            return {update(k): update(v) for k, v in value.items()}
        if isinstance(value, list):
            return [update(v) for v in value]
        if isinstance(value, str):
            for old, new in pairs:
                value = re.sub(re.escape(old), lambda _: new, value, flags=re.IGNORECASE)
        return value

    info_file = new_state / "repo.json"
    info = json.loads(info_file.read_text(encoding="utf-8"))
    info.update(common_dir=str(new_repo / ".git"), repo_id=new_state.name)
    info_file.write_text(json.dumps(info, ensure_ascii=False) + "\n", encoding="utf-8")
    for path in new_state.rglob("*"):
        if not path.is_file() or path == info_file:
            continue
        if path.suffix in (".json", ".jsonl"):
            lines = path.read_text(encoding="utf-8").splitlines()
            if path.suffix == ".json":
                value = update(json.loads("\n".join(lines)))
                path.write_text(json.dumps(value, ensure_ascii=False) + "\n", encoding="utf-8")
            else:
                clean = [json.dumps(update(json.loads(line)), ensure_ascii=False) for line in lines if line.strip()]
                path.write_text("".join(line + "\n" for line in clean), encoding="utf-8")
        elif path.suffix in (".md", ".txt"):
            contents = path.read_text(encoding="utf-8")
            changed = update(contents)
            if changed != contents:
                path.write_text(changed, encoding="utf-8")
    with closing(sqlite3.connect(new_state / "state.sqlite")) as db, db:
        tables = db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").fetchall()
        for (table,) in tables:
            tab = quote(table)
            columns = db.execute(f"PRAGMA table_info({tab})").fetchall()
            for _, column, *_ in columns:
                col = quote(column)
                # Replace only text containing a changed path. Quoted JSON contains escaped backslashes.
                for old, new in [*pairs, *((json.dumps(a)[1:-1], json.dumps(b, ensure_ascii=False)[1:-1])
                                           for a, b in pairs)]:
                    if old != new:
                        db.execute(f"UPDATE {tab} SET {col}=replace({col}, ?, ?) "
                                   f"WHERE typeof({col})='text' AND instr({col}, ?) > 0",
                                   (old, new, old))
