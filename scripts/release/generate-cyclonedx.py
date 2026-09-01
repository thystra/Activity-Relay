#!/usr/bin/env python3
import argparse
import datetime as dt
import hashlib
import json
import pathlib
import subprocess
import urllib.parse
import uuid


def read_json_stream(text):
    decoder = json.JSONDecoder()
    pos = 0
    out = []
    while True:
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            return out
        obj, end = decoder.raw_decode(text, pos)
        out.append(obj)
        pos = end


p = argparse.ArgumentParser()
p.add_argument("--version", required=True)
p.add_argument("--debian-version", required=True)
p.add_argument("--binary", required=True)
p.add_argument("--source-date-epoch", type=int, required=True)
p.add_argument("--source-identity", required=True)
p.add_argument("--output", required=True)
a = p.parse_args()

binary = pathlib.Path(a.binary)
digest = hashlib.sha256(binary.read_bytes()).hexdigest()
modules = read_json_stream(
    subprocess.check_output(["go", "list", "-m", "-json", "all"], text=True)
)

root_ref = "pkg:generic/activity-relay@" + urllib.parse.quote(
    a.version, safe=".-_~"
)
components = []
refs = []
for module in modules:
    if module.get("Main"):
        continue
    path = module.get("Path", "").strip()
    version = module.get("Version", "").strip() or "unknown"
    if not path:
        continue
    purl = (
        "pkg:golang/"
        + urllib.parse.quote(path, safe="/.-_~")
        + "@"
        + urllib.parse.quote(version, safe=".-_~+")
    )
    component = {
        "bom-ref": purl,
        "type": "library",
        "name": path,
        "version": version,
        "purl": purl,
    }
    replace = module.get("Replace")
    if isinstance(replace, dict) and replace.get("Path"):
        component["properties"] = [
            {
                "name": "activity-relay:go-replacement-path",
                "value": str(replace["Path"]),
            },
            {
                "name": "activity-relay:go-replacement-version",
                "value": str(replace.get("Version", "")),
            },
        ]
    components.append(component)
    refs.append(purl)

components.sort(key=lambda item: (item["name"], item["version"]))
refs = sorted(refs)

timestamp = (
    dt.datetime.fromtimestamp(a.source_date_epoch, dt.timezone.utc)
    .isoformat()
    .replace("+00:00", "Z")
)
serial = uuid.uuid5(
    uuid.NAMESPACE_URL,
    f"https://forgejo.argentwolf.org/alan/activity-relay/sbom/{a.version}/{a.source_identity}",
)

bom = {
    "bomFormat": "CycloneDX",
    "specVersion": "1.6",
    "serialNumber": f"urn:uuid:{serial}",
    "version": 1,
    "metadata": {
        "timestamp": timestamp,
        "component": {
            "bom-ref": root_ref,
            "type": "application",
            "name": "activity-relay",
            "version": a.version,
            "purl": root_ref,
            "hashes": [{"alg": "SHA-256", "content": digest}],
            "properties": [
                {
                    "name": "activity-relay:debian-version",
                    "value": a.debian_version,
                },
                {
                    "name": "activity-relay:source-identity",
                    "value": a.source_identity,
                },
            ],
        },
    },
    "components": components,
    "dependencies": [{"ref": root_ref, "dependsOn": refs}],
}
pathlib.Path(a.output).write_text(
    json.dumps(bom, sort_keys=True, separators=(",", ":")) + "\n"
)
