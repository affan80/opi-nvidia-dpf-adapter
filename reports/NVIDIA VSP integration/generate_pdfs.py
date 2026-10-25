#!/usr/bin/env python3
"""Render the NVIDIA VSP integration reports to PDF."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import letter
from reportlab.lib.units import inch
from reportlab.platypus import SimpleDocTemplate

ROOT = Path(__file__).resolve().parent
SRC = Path("/Users/kartikey0104/opi_test_report/Lifecycle Manager/generate_pdfs.py")


def load_renderer():
    spec = importlib.util.spec_from_file_location("lcm_pdf", SRC)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def footer(canvas, doc):
    canvas.saveState()
    canvas.setStrokeColorRGB(11 / 255, 61 / 255, 92 / 255)
    canvas.setLineWidth(1.2)
    canvas.line(0.7 * inch, 0.55 * inch, 7.8 * inch, 0.55 * inch)
    canvas.setFont("Times-Roman", 8)
    canvas.setFillColorRGB(11 / 255, 61 / 255, 92 / 255)
    canvas.drawString(
        0.7 * inch,
        0.38 * inch,
        "OPI test report - NVIDIA VSP integration - 2026-10-25",
    )
    canvas.drawRightString(7.8 * inch, 0.38 * inch, f"Page {doc.page}")
    canvas.restoreState()


def render(mod, md_name: str, pdf_name: str):
    st = mod.styles()
    st["body"].alignment = TA_LEFT
    st["li"].alignment = TA_LEFT
    md = (ROOT / md_name).read_text(encoding="utf-8")
    doc = SimpleDocTemplate(
        str(ROOT / pdf_name),
        pagesize=letter,
        leftMargin=0.7 * inch,
        rightMargin=0.7 * inch,
        topMargin=0.65 * inch,
        bottomMargin=0.7 * inch,
        title=pdf_name.replace(".pdf", ""),
        author="OPI DPF adapter / NVIDIA VSP integration",
    )
    doc.build(mod.md_to_flowables(md, st), onFirstPage=footer, onLaterPages=footer)
    print("wrote", ROOT / pdf_name)


def main():
    mod = load_renderer()
    pairs = [
        ("01_test_commands.md", "01_test_commands.pdf"),
        ("02_issues_and_fixes.md", "02_issues_and_fixes.pdf"),
        ("03_full_report.md", "03_full_report.pdf"),
    ]
    for md, pdf in pairs:
        render(mod, md, pdf)


if __name__ == "__main__":
    main()
