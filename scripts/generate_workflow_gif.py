#!/usr/bin/env python3
"""Generate a terminal-style dramatization of a worktree-manager workflow."""

from __future__ import annotations

import argparse
import html
import math
import shutil
import subprocess
import tempfile
from pathlib import Path

WIDTH = 1200
HEIGHT = 700
BG = "#0b1220"
SURFACE = "#111c30"
SURFACE_ALT = "#16243c"
TERMINAL = "#050b16"
BORDER = "#263854"
WHITE = "#f8fafc"
MUTED = "#91a4bf"
DIM = "#627795"
BLUE = "#60a5fa"
CYAN = "#22d3ee"
GREEN = "#34d399"
YELLOW = "#fbbf24"
PINK = "#f472b6"
RED = "#fb7185"
MONO = "Menlo, SFMono-Regular, Consolas, monospace"
SANS = "-apple-system, BlinkMacSystemFont, Helvetica, Arial, sans-serif"



def esc(value: object) -> str:
    return html.escape(str(value), quote=True)



def rect(x: float, y: float, w: float, h: float, fill: str, radius: float = 0, stroke: str | None = None, stroke_width: float = 1, opacity: float = 1) -> str:
    attrs = [
        f'x="{x}"',
        f'y="{y}"',
        f'width="{w}"',
        f'height="{h}"',
        f'fill="{fill}"',
        f'opacity="{opacity}"',
    ]
    if radius:
        attrs.append(f'rx="{radius}"')
        attrs.append(f'ry="{radius}"')
    if stroke:
        attrs.append(f'stroke="{stroke}"')
        attrs.append(f'stroke-width="{stroke_width}"')
    return f'<rect {" ".join(attrs)} />'



def line(x1: float, y1: float, x2: float, y2: float, stroke: str, width: float = 1, opacity: float = 1, dash: str | None = None) -> str:
    del dash
    dx = x2 - x1
    dy = y2 - y1
    length = math.hypot(dx, dy) or 1
    normal_x = -dy / length * width / 2
    normal_y = dx / length * width / 2
    points = (
        f"{x1 + normal_x},{y1 + normal_y} "
        f"{x2 + normal_x},{y2 + normal_y} "
        f"{x2 - normal_x},{y2 - normal_y} "
        f"{x1 - normal_x},{y1 - normal_y}"
    )
    return f'<polygon points="{points}" fill="{stroke}" opacity="{opacity}" />'



def circle(cx: float, cy: float, radius: float, fill: str, stroke: str | None = None, stroke_width: float = 1, opacity: float = 1) -> str:
    result = ""
    if stroke:
        result += f'<circle cx="{cx}" cy="{cy}" r="{radius}" fill="{stroke}" opacity="{opacity}" />'
        inner_radius = max(0, radius - stroke_width)
        if fill != "none" and inner_radius:
            result += f'<circle cx="{cx}" cy="{cy}" r="{inner_radius}" fill="{fill}" opacity="{opacity}" />'
        return result
    return f'<circle cx="{cx}" cy="{cy}" r="{radius}" fill="{fill}" opacity="{opacity}" />'



def text(x: float, y: float, value: str, size: float = 16, fill: str = WHITE, family: str = MONO, weight: str = "400", anchor: str = "start", opacity: float = 1, letter_spacing: float | None = None) -> str:
    spacing = f' letter-spacing="{letter_spacing}px"' if letter_spacing is not None else ""
    return (
        f'<text x="{x}" y="{y}" fill="{fill}" font-family="{family}" '
        f'font-size="{size}px" font-weight="{weight}" text-anchor="{anchor}" '
        f'opacity="{opacity}"{spacing}>{esc(value)}</text>'
    )



def multiline(x: float, y: float, values: list[str], size: float = 16, fill: str = WHITE, family: str = MONO, line_height: float = 25, weight: str = "400") -> str:
    return "".join(text(x, y + index * line_height, value, size, fill, family, weight) for index, value in enumerate(values))



def pill(x: float, y: float, label: str, fill: str, color: str = WHITE, width: float | None = None, border: str | None = None) -> str:
    calculated_width = width if width is not None else max(72, len(label) * 8.1 + 28)
    result = rect(x, y, calculated_width, 28, fill, 14, border or fill, 1)
    result += text(x + calculated_width / 2, y + 19, label, 11, color, SANS, "700", "middle", 1, 0.8)
    return result



def cursor(x: float, y: float, width: float = 9, height: float = 20, visible: bool = True) -> str:
    if not visible:
        return ""
    return rect(x, y - height + 4, width, height, CYAN, 1, None, 1, 0.9)



def logo(x: float, y: float, scale: float = 1) -> str:
    result = ""
    result += line(x + 7 * scale, y + 15 * scale, x + 18 * scale, y + 8 * scale, CYAN, 2 * scale)
    result += line(x + 18 * scale, y + 8 * scale, x + 29 * scale, y + 15 * scale, BLUE, 2 * scale)
    result += line(x + 18 * scale, y + 8 * scale, x + 18 * scale, y + 23 * scale, PINK, 2 * scale)
    result += circle(x + 7 * scale, y + 15 * scale, 4 * scale, BG, CYAN, 2 * scale)
    result += circle(x + 18 * scale, y + 8 * scale, 4 * scale, BG, BLUE, 2 * scale)
    result += circle(x + 29 * scale, y + 15 * scale, 4 * scale, BG, BLUE, 2 * scale)
    result += circle(x + 18 * scale, y + 23 * scale, 4 * scale, BG, PINK, 2 * scale)
    return result



def background() -> str:
    result = rect(0, 0, WIDTH, HEIGHT, BG)
    for x in range(30, WIDTH, 36):
        for y in range(30, HEIGHT, 36):
            result += circle(x, y, 1, "#1a2a42", opacity=0.28)
    result += rect(0, 0, WIDTH, 8, CYAN, 0, None, 1, 0.8)
    return result



def header() -> str:
    result = logo(50, 25, 1.15)
    result += text(94, 49, "worktree-manager", 22, WHITE, SANS, "700")
    result += text(94, 68, "reusable worktrees for autonomous agents", 11, MUTED, SANS, "400")
    result += pill(922, 28, "DRAMATIZATION", "#203451", CYAN, 150, "#365275")
    result += text(1110, 48, "not a live recording", 11, MUTED, SANS, "400", "middle")
    return result



def timeline(active: str | None) -> str:
    steps = [
        (90, "ACQUIRE", "acquire"),
        (430, "WORK", "work"),
        (790, "RELEASE", "release"),
        (1110, "READY", "ready"),
    ]
    result = line(90, 113, 1110, 113, BORDER, 2)
    for x, label, key in steps:
        is_active = key == active
        is_done = active is not None and [item[2] for item in steps].index(key) < [item[2] for item in steps].index(active)
        color = GREEN if is_done else (CYAN if is_active else DIM)
        result += circle(x, 113, 9 if is_active else 7, color if (is_active or is_done) else BG, color, 2)
        if is_done:
            result += text(x, 117, "✓", 11, BG, SANS, "700", "middle")
        result += text(x, 138, label, 10, color, SANS, "700", "middle", 1, 1.2)
    return result



def window(x: float, y: float, w: float, h: float, title: str) -> str:
    result = rect(x, y, w, h, TERMINAL, 12, BORDER, 1)
    result += rect(x, y, w, 38, SURFACE_ALT, 12, None)
    result += rect(x, y + 26, w, 12, SURFACE_ALT)
    result += circle(x + 21, y + 19, 5, RED)
    result += circle(x + 39, y + 19, 5, YELLOW)
    result += circle(x + 57, y + 19, 5, GREEN)
    result += text(x + 82, y + 23, title, 12, MUTED, MONO, "400")
    return result



def terminal_shell(phase: str, value: int, frame: int) -> str:
    x, y, w, h = 48, 166, 724, 480
    result = window(x, y, w, h, "agent-shell  /  isolated-worktree")
    tx = x + 27
    line_y = y + 84
    command = "worktree-manager acquire BenE/add-unit-menu"
    if phase == "acquire_type":
        shown = command[:value]
        result += text(tx, line_y, "$", 17, GREEN)
        result += text(tx + 21, line_y, shown, 17, WHITE)
        cursor_x = tx + 21 + len(shown) * 10.1
        result += cursor(cursor_x, line_y, visible=frame % 2 == 0)
        result += text(tx, line_y + 53, "requesting a clean workspace for the next task", 14, MUTED)
        result += text(tx, line_y + 89, "stdout will be the worktree path", 14, DIM)
        return result

    if phase in {"acquire_process", "acquire_done"}:
        result += text(tx, line_y, "$", 17, GREEN)
        result += text(tx + 21, line_y, command, 17, WHITE)
        process_lines = [
            ("◷", "locating a reusable worktree", MUTED),
            ("✓", "fetched origin/main", GREEN),
            ("✓", "reset and cleaned pool slot", GREEN),
            ("✓", "dependencies ready  (go mod download)", GREEN),
        ]
        visible = value if phase == "acquire_process" else len(process_lines)
        for index, (mark, label, color) in enumerate(process_lines[:visible]):
            result += text(tx + 3, line_y + 53 + index * 32, mark, 15, color, MONO, "700")
            result += text(tx + 29, line_y + 53 + index * 32, label, 14, color)
        if phase == "acquire_done":
            path_y = line_y + 53 + len(process_lines) * 32
            result += line(tx + 1, path_y - 19, x + w - 27, path_y - 19, BORDER, 1)
            result += text(tx, path_y + 10, ">", 17, CYAN, "Menlo, monospace", "700")
            result += text(tx + 22, path_y + 10, "/private/tmp/worktree-manager/repo-app/pool-app-1-1", 14, WHITE)
            result += pill(tx, path_y + 29, "ALLOCATED", "#123d45", GREEN, 98, "#1e6870")
            result += text(tx + 113, path_y + 48, "branch: BenE/add-unit-menu", 13, MUTED)
            result += text(tx, path_y + 80, "# you get an up to date worktree path", 14, CYAN)
        return result

    if phase == "work":
        stages = [
            ("$", "cd /private/tmp/worktree-manager/repo-app/pool-app-1-1", WHITE),
            ("$", "git status --short", WHITE),
            ("✓", "clean checkout  /  branch: BenE/add-unit-menu", GREEN),
            ("$", "vim internal/search/search.go", WHITE),
            ("#", "you do your changes", CYAN),
            ("$", 'git commit -am "Add search support"\u200b', WHITE),
            ("✓", "[BenE/add-unit-menu 7c2a1d] Add search support", GREEN),
            ("$", "git push -u origin BenE/add-unit-menu", WHITE),
            ("✓", "pushed BenE/add-unit-menu to origin", GREEN),
        ]
        max_lines = min(len(stages), value)
        for index, (mark, label, color) in enumerate(stages[:max_lines]):
            row_y = line_y + index * 32
            result += text(tx, row_y, mark, 16, GREEN if mark == "$" else color, MONO, "700")
            result += text(tx + 22, row_y, label, 14, color)
        if value >= 1 and value < len(stages):
            last_label = stages[value - 1][1]
            result += cursor(tx + 22 + len(last_label) * 8.45, line_y + (value - 1) * 32, 8, 18, frame % 2 == 0)
        if value >= len(stages):
            result += pill(tx, line_y + 306, "READY TO RELEASE", "#123d45", GREEN, 142, "#1e6870")
        return result

    if phase == "release_type":
        release_command = 'worktree-manager release "$WT"\u200b'
        shown = release_command[:value]
        result += text(tx, line_y, "$", 17, GREEN)
        result += text(tx + 21, line_y, shown, 17, WHITE)
        cursor_x = tx + 21 + len(shown) * 10.1
        result += cursor(cursor_x, line_y, visible=frame % 2 == 0)
        result += text(tx, line_y + 53, "return the finished workspace to the pool", 14, MUTED)
        result += text(tx, line_y + 89, "the task branch will be removed", 14, DIM)
        return result

    if phase == "release_process":
        release_command = 'worktree-manager release "$WT"\u200b'
        result += text(tx, line_y, "$", 17, GREEN)
        result += text(tx + 21, line_y, release_command, 17, WHITE)
        process_lines = [
            ("✓", "fetched origin/main", GREEN),
            ("✓", "reset --hard origin/main", GREEN),
            ("✓", "cleaned files and submodules", GREEN),
            ("✓", "detached and verified clean", GREEN),
            ("✓", "released", CYAN),
        ]
        for index, (mark, label, color) in enumerate(process_lines[:value]):
            row_y = line_y + 53 + index * 32
            result += text(tx + 3, row_y, mark, 15, color, MONO, "700")
            result += text(tx + 29, row_y, label, 14, color)
        return result

    if phase == "ready":
        result += text(tx, line_y, "$", 17, GREEN)
        result += text(tx + 21, line_y, "worktree-manager list", 17, WHITE)
        result += text(tx, line_y + 57, "FREE      -       .../pool-app-1-1", 14, GREEN)
        result += text(tx, line_y + 91, "FREE      -       .../pool-app-1-2", 14, GREEN)
        result += line(tx, line_y + 120, x + w - 27, line_y + 120, BORDER, 1)
        result += text(tx, line_y + 159, "pool is clean and ready for the next task", 15, WHITE)
        result += pill(tx, line_y + 194, "REPEAT", "#17364d", CYAN, 88, "#285d78")
        return result

    return result



def status_dot(x: float, y: float, color: str, label: str, detail: str, active: bool = False) -> str:
    result = circle(x, y, 7, color if active else BG, color, 2)
    if active:
        result += circle(x, y, 12, "none", color, 1, 0.25)
    result += text(x + 22, y - 2, label, 13, WHITE, SANS, "700")
    result += text(x + 22, y + 17, detail, 11, MUTED, SANS)
    return result



def pool_panel(phase: str, frame: int) -> str:
    x, y, w, h = 798, 166, 354, 480
    result = rect(x, y, w, h, SURFACE, 12, BORDER, 1)
    result += text(x + 25, y + 35, "WORKTREE POOL", 12, CYAN, SANS, "700", letter_spacing=1.5)
    result += text(x + 25, y + 58, "repo-app", 17, WHITE, MONO, "700")
    result += text(x + w - 25, y + 58, "SQLite state", 11, MUTED, SANS, "400", "end")

    if phase in {"acquire_type", "acquire_process"}:
        active = phase == "acquire_process"
        result += line(x + 44, y + 118, x + 44, y + 334, BORDER, 2)
        result += status_dot(x + 44, y + 104, BLUE, "main", "primary checkout")
        result += status_dot(x + 44, y + 207, CYAN if active else DIM, "pool-app-1-1", "finding a free slot", active)
        result += status_dot(x + 44, y + 310, DIM, "pool-app-1-2", "FREE  /  available")
        result += pill(x + 25, y + 391, "ISOLATED BY GIT", "#1d2941", BLUE, 131, "#33496d")
        result += text(x + 25, y + 443, "A task gets its own checkout", 13, MUTED, SANS)
        result += text(x + 25, y + 464, "before the agent starts.", 13, MUTED, SANS)
        return result

    allocated = phase in {"acquire_done", "work", "release_type", "release_process"}
    releasing = phase in {"release_type", "release_process"}
    ready = phase == "ready"
    result += line(x + 44, y + 118, x + 44, y + 334, BORDER, 2)
    result += status_dot(x + 44, y + 104, BLUE, "main", "primary checkout")
    slot_color = YELLOW if releasing else (GREEN if ready else CYAN)
    slot_label = "pool-app-1-1"
    slot_detail = "FREE  /  ready to reuse" if ready else ("cleaning and detaching" if releasing else "ALLOCATED  /  task branch")
    result += status_dot(x + 44, y + 207, slot_color, slot_label, slot_detail, allocated and not ready)
    result += status_dot(x + 44, y + 310, GREEN if ready else DIM, "pool-app-1-2", "FREE  /  available", ready)

    if phase == "acquire_done":
        result += pill(x + 25, y + 391, "ALLOCATED", "#123d45", GREEN, 104, "#1e6870")
        result += text(x + 25, y + 443, "branch: BenE/add-unit-menu", 12, MUTED, MONO)
    elif phase == "work":
        result += pill(x + 25, y + 391, "AGENT WORKING", "#17364d", CYAN, 130, "#285d78")
        result += text(x + 25, y + 443, "changes stay out of main", 13, MUTED, SANS)
    elif releasing:
        result += pill(x + 25, y + 391, "RESETTING", "#463a1d", YELLOW, 104, "#7b652c")
        result += text(x + 25, y + 443, "fresh snapshot on return", 13, MUTED, SANS)
    elif ready:
        result += pill(x + 25, y + 391, "READY", "#123d45", GREEN, 83, "#1e6870")
        result += text(x + 25, y + 443, "clean slot for the next task", 13, MUTED, SANS)
    return result



def main_frame(phase: str, value: int, frame: int) -> str:
    result = background()
    result += header()
    result += timeline({"acquire_type": "acquire", "acquire_process": "acquire", "acquire_done": "acquire", "work": "work", "release_type": "release", "release_process": "release", "ready": "ready"}.get(phase))
    result += terminal_shell(phase, value, frame)
    result += pool_panel(phase, frame)
    return result




def svg_document(body: str) -> str:
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{WIDTH}" height="{HEIGHT}" '
        f'viewBox="0 0 {WIDTH} {HEIGHT}">{body}</svg>'
    )



def build_frames() -> list[str]:
    frames: list[str] = []
    frame_number = 0

    def add_main(phase: str, value: int, count: int = 1) -> None:
        nonlocal frame_number
        for _ in range(count):
            frames.append(svg_document(main_frame(phase, value, frame_number)))
            frame_number += 1

    acquire_command_length = len("worktree-manager acquire BenE/add-unit-menu")
    for position in range(0, acquire_command_length + 1, 3):
        add_main("acquire_type", min(position, acquire_command_length), 1)
    add_main("acquire_type", acquire_command_length, 5)

    for visible in range(1, 5):
        add_main("acquire_process", visible, 2)
    add_main("acquire_done", 0, 14)

    work_holds = [2, 2, 3, 8, 18, 8, 12, 8, 20]
    for visible, hold in enumerate(work_holds, start=1):
        add_main("work", visible, hold)

    release_command_length = len('worktree-manager release "$WT"\u200b')
    for position in range(0, release_command_length + 1, 3):
        add_main("release_type", min(position, release_command_length), 1)
    add_main("release_type", release_command_length, 4)
    for visible in range(1, 6):
        add_main("release_process", visible, 4)
    add_main("release_process", 5, 8)

    add_main("ready", 0, 15)
    return frames



def create_gif(output: Path) -> None:
    magick = shutil.which("magick") or shutil.which("convert")
    if not magick:
        raise SystemExit("ImageMagick (magick or convert) is required")

    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="worktree-manager-gif-") as temp_dir:
        frame_dir = Path(temp_dir)
        for index, svg in enumerate(build_frames()):
            (frame_dir / f"frame-{index:03d}.svg").write_text(svg, encoding="utf-8")

        frame_paths = [str(path) for path in sorted(frame_dir.glob("frame-*.svg"))]
        command = [
            magick,
            "-background",
            BG,
            "-density",
            "96",
            "-delay",
            "10",
            "-loop",
            "0",
            *frame_paths,
            "-layers",
            "Optimize",
            "-colors",
            "256",
            "-strip",
            str(output),
        ]
        subprocess.run(command, check=True)



def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("-o", "--output", type=Path, default=Path("docs/workflow.gif"))
    args = parser.parse_args()
    create_gif(args.output)
    print(f"wrote {args.output}")


if __name__ == "__main__":
    main()
