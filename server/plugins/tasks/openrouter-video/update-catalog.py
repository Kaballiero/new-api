#!/usr/bin/env python3
"""Refresh public model capabilities outside synchronous plugin hooks."""
import datetime
import json
import pathlib
import urllib.request

url = "https://openrouter.ai/api/v1/videos/models"
with urllib.request.urlopen(url, timeout=30) as response:
    models = json.load(response)["data"]
with urllib.request.urlopen("https://openrouter.ai/api/v1/models?output_modalities=video", timeout=30) as response:
    modalities = {model["id"]: model["architecture"]["input_modalities"] for model in json.load(response)["data"]}
if not models or len({model["id"] for model in models}) != len(models):
    raise ValueError("The video catalog is empty or contains duplicate IDs")
fields = ["id", "canonical_slug", "supported_durations", "supported_resolutions", "supported_aspect_ratios", "supported_sizes", "supported_frame_images", "generate_audio", "seed", "upscale_factor", "creativity", "allowed_passthrough_parameters", "pricing_skus"]
catalog = {
    "fetched_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "models": [{field: model.get(field) for field in fields} for model in models],
}
for model in catalog["models"]:
    model["input_modalities"] = modalities[model["id"]]
path = pathlib.Path(__file__).with_name("plugin.js")
source = path.read_text()
start = source.index("// BEGIN VIDEO CATALOG")
end = source.index("// END VIDEO CATALOG", start)
block = "// BEGIN VIDEO CATALOG\nconst catalog = " + json.dumps(catalog, ensure_ascii=False, indent=2) + ";\n"
path.write_text(source[:start] + block + source[end:])
print(f"Refreshed {len(models)} video model records; review capabilities and pricing before use")
