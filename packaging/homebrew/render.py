#!/usr/bin/env python3
"""Render the release formula from the checked-in template and exact assets."""
import pathlib
import re
import sys


def render(tag, checksums):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        raise ValueError("expected a stable vMAJOR.MINOR.PATCH tag")
    values = {}
    for line in checksums.splitlines():
        parts = line.split()
        if len(parts) != 2:
            raise ValueError("invalid checksum line")
        sha, asset = parts
        if not re.fullmatch(r"[a-fA-F0-9]{64}", sha):
            raise ValueError("invalid SHA256")
        if asset in values:
            raise ValueError("duplicate asset checksum")
        values[asset] = sha.lower()
    text = pathlib.Path(__file__).with_name("mav.rb").read_text()
    for key, value in {
        "VERSION": tag[1:],
        "ARM64_SHA": values["mav-darwin-arm64"],
        "AMD64_SHA": values["mav-darwin-amd64"],
    }.items():
        text = text.replace("@" + key + "@", value)
    return text


if __name__ == "__main__":
    if len(sys.argv) != 4:
        sys.exit("usage: render.py TAG CHECKSUMS OUTPUT")
    result = render(sys.argv[1], pathlib.Path(sys.argv[2]).read_text())
    pathlib.Path(sys.argv[3]).write_text(result)
