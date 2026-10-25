#!/usr/bin/env python3
"""Render the Lifecycle Manager markdown reports to PDF (reportlab)."""

from __future__ import annotations

import re
from pathlib import Path

from reportlab.lib import colors
from reportlab.lib.enums import TA_JUSTIFY, TA_LEFT
from reportlab.lib.pagesizes import letter
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import inch
from reportlab.platypus import (
    ListFlowable,
    ListItem,
    PageBreak,
    Paragraph,
    Preformatted,
    SimpleDocTemplate,
    Spacer,
    Table,
    TableStyle,
)

ROOT = Path(__file__).resolve().parent
NAVY = colors.HexColor("#0b3d5c")
TEAL = colors.HexColor("#1a6b7a")
RULE = colors.HexColor("#c5d0d6")
CODE_BG = colors.HexColor("#f4f6f7")
ROW = colors.HexColor("#eef3f6")


def styles():
    base = getSampleStyleSheet()
    return {
        "title": ParagraphStyle(
            "T",
            parent=base["Title"],
            fontName="Times-Bold",
            fontSize=18,
            textColor=NAVY,
            spaceAfter=8,
            leading=22,
            alignment=TA_LEFT,
        ),
        "h1": ParagraphStyle(
            "H1",
            parent=base["Heading1"],
            fontName="Times-Bold",
            fontSize=14,
            textColor=NAVY,
            spaceBefore=14,
            spaceAfter=6,
            leading=18,
        ),
        "h2": ParagraphStyle(
            "H2",
            parent=base["Heading2"],
            fontName="Times-Bold",
            fontSize=12,
            textColor=TEAL,
            spaceBefore=10,
            spaceAfter=4,
            leading=15,
        ),
        "h3": ParagraphStyle(
            "H3",
            parent=base["Heading3"],
            fontName="Times-Bold",
            fontSize=11,
            textColor=NAVY,
            spaceBefore=8,
            spaceAfter=3,
            leading=14,
        ),
        "body": ParagraphStyle(
            "B",
            parent=base["Normal"],
            fontName="Times-Roman",
            fontSize=10,
            leading=13,
            alignment=TA_JUSTIFY,
            spaceAfter=6,
        ),
        "code": ParagraphStyle(
            "C",
            parent=base["Code"],
            fontName="Courier",
            fontSize=8,
            leading=10,
            backColor=CODE_BG,
            leftIndent=4,
            rightIndent=4,
            spaceBefore=4,
            spaceAfter=8,
        ),
        "cell": ParagraphStyle(
            "Cell",
            parent=base["Normal"],
            fontName="Times-Roman",
            fontSize=8,
            leading=10,
        ),
        "th": ParagraphStyle(
            "TH",
            parent=base["Normal"],
            fontName="Times-Bold",
            fontSize=8,
            leading=10,
            textColor=colors.white,
        ),
        "footer": ParagraphStyle(
            "F",
            parent=base["Normal"],
            fontName="Times-Roman",
            fontSize=8,
            textColor=colors.HexColor("#555555"),
        ),
        "li": ParagraphStyle(
            "LI",
            parent=base["Normal"],
            fontName="Times-Roman",
            fontSize=10,
            leading=13,
            leftIndent=8,
        ),
    }


def esc(s: str) -> str:
    s = s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
    s = re.sub(r"`([^`]+)`", r"<font face='Courier' size='8'>\1</font>", s)
    s = re.sub(r"\*\*([^*]+)\*\*", r"<b>\1</b>", s)
    s = s.replace("|", "&#124;")
    return s


def cell(text: str, header: bool, st) -> Paragraph:
    return Paragraph(esc(text.strip()), st["th"] if header else st["cell"])


def parse_table(lines: list[str], st) -> Table:
    rows = []
    for i, line in enumerate(lines):
        cols = [c.strip() for c in line.strip().strip("|").split("|")]
        if i == 1 and all(set(c) <= set("-: ") and c for c in cols):
            continue
        header = i == 0
        rows.append([cell(c, header, st) for c in cols])
    coln = max(len(r) for r in rows)
    for r in rows:
        while len(r) < coln:
            r.append(Paragraph("", st["cell"]))
    w = 7.0 * inch / coln
    t = Table(rows, colWidths=[w] * coln, repeatRows=1)
    t.setStyle(
        TableStyle(
            [
                ("BACKGROUND", (0, 0), (-1, 0), NAVY),
                ("TEXTCOLOR", (0, 0), (-1, 0), colors.white),
                ("FONTNAME", (0, 0), (-1, 0), "Times-Bold"),
                ("BACKGROUND", (0, 1), (-1, -1), colors.white),
                ("ROWBACKGROUNDS", (0, 1), (-1, -1), [colors.white, ROW]),
                ("GRID", (0, 0), (-1, -1), 0.3, RULE),
                ("VALIGN", (0, 0), (-1, -1), "TOP"),
                ("LEFTPADDING", (0, 0), (-1, -1), 4),
                ("RIGHTPADDING", (0, 0), (-1, -1), 4),
                ("TOPPADDING", (0, 0), (-1, -1), 3),
                ("BOTTOMPADDING", (0, 0), (-1, -1), 3),
            ]
        )
    )
    return t


def md_to_flowables(md: str, st):
    story = []
    lines = md.splitlines()
    i = 0
    first_h1 = True
    while i < len(lines):
        line = lines[i]
        if line.startswith("```"):
            buf = []
            i += 1
            while i < len(lines) and not lines[i].startswith("```"):
                buf.append(lines[i])
                i += 1
            i += 1
            story.append(
                Preformatted("\n".join(buf) if buf else " ", st["code"])
            )
            continue
        if line.startswith("|") and i + 1 < len(lines) and lines[i + 1].startswith("|"):
            tbl = []
            while i < len(lines) and lines[i].startswith("|"):
                tbl.append(lines[i])
                i += 1
            story.append(parse_table(tbl, st))
            story.append(Spacer(1, 8))
            continue
        if line.strip() == "---":
            story.append(Spacer(1, 6))
            i += 1
            continue
        if line.startswith("# "):
            style = st["title"] if first_h1 else st["h1"]
            first_h1 = False
            story.append(Paragraph(esc(line[2:].strip()), style))
            i += 1
            continue
        if line.startswith("## "):
            story.append(Paragraph(esc(line[3:].strip()), st["h1"]))
            i += 1
            continue
        if line.startswith("### "):
            story.append(Paragraph(esc(line[4:].strip()), st["h2"]))
            i += 1
            continue
        if line.startswith("- "):
            items = []
            while i < len(lines) and lines[i].startswith("- "):
                items.append(
                    ListItem(Paragraph(esc(lines[i][2:].strip()), st["li"]))
                )
                i += 1
            story.append(
                ListFlowable(items, bulletType="bullet", leftIndent=16, spaceAfter=6)
            )
            continue
        if not line.strip():
            i += 1
            continue
        para = [line.strip()]
        i += 1
        while i < len(lines) and lines[i].strip() and not lines[i].startswith(
            ("#", "|", "-", "```", "---")
        ):
            para.append(lines[i].strip())
            i += 1
        story.append(Paragraph(esc(" ".join(para)), st["body"]))
    return story


def footer(canvas, doc):
    canvas.saveState()
    canvas.setStrokeColor(NAVY)
    canvas.setLineWidth(1.2)
    canvas.line(0.7 * inch, 0.55 * inch, 7.8 * inch, 0.55 * inch)
    canvas.setFont("Times-Roman", 8)
    canvas.setFillColor(NAVY)
    canvas.drawString(0.7 * inch, 0.38 * inch, "OPI test report — Lifecycle Manager — 2026-10-25")
    canvas.drawRightString(7.8 * inch, 0.38 * inch, f"Page {doc.page}")
    canvas.restoreState()


def render(md_name: str, pdf_name: str):
    st = styles()
    md = (ROOT / md_name).read_text(encoding="utf-8")
    doc = SimpleDocTemplate(
        str(ROOT / pdf_name),
        pagesize=letter,
        leftMargin=0.7 * inch,
        rightMargin=0.7 * inch,
        topMargin=0.65 * inch,
        bottomMargin=0.7 * inch,
        title=md_name.replace(".md", ""),
        author="OPI DPF adapter / Lifecycle Manager",
    )
    doc.build(md_to_flowables(md, st), onFirstPage=footer, onLaterPages=footer)
    print("wrote", ROOT / pdf_name)


def main():
    pairs = [
        ("01_lcm_fix_build_tests.md", "01_lcm_fix_build_tests.pdf"),
        ("02_security_cves.md", "02_security_cves.pdf"),
        ("03_issues_and_fixes.md", "03_issues_and_fixes.pdf"),
        ("04_e2e_report.md", "04_e2e_report.pdf"),
    ]
    for md, pdf in pairs:
        render(md, pdf)


if __name__ == "__main__":
    main()
