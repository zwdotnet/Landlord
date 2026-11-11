"""Export one image and its exact frontend; never include runtime data or secrets."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def web_manifest(directory):
    return {"web/" + p.relative_to(directory).as_posix(): sha(p)
            for p in sorted(directory.rglob("*")) if p.is_file()}


image = os.environ["LANDLORD_IMAGE"]
if not re.fullmatch(r"landlord:[A-Za-z0-9_.-]+", image):
    raise SystemExit("Invalid image tag")
output = Path(sys.argv[1])
output.mkdir(parents=True, exist_ok=False)
info = json.loads(run("docker", "image", "inspect", image))[0]
if (info["Os"], info["Architecture"]) != ("linux", "amd64"):
    raise SystemExit("Expected linux/amd64")
commit = run("git", "rev-parse", "HEAD")
if commit != os.environ["GITHUB_SHA"]:
    raise SystemExit("Checkout does not match workflow commit")
with tempfile.TemporaryDirectory(prefix="landlord-package-") as temporary:
    stage = Path(temporary)
    container = run("docker", "create", image)
    try:
        run("docker", "cp", container + ":/app/web", str(stage / "web"))
    finally:
        run("docker", "rm", container)
    manifest = web_manifest(stage / "web")
    if manifest != web_manifest(Path("web/dist")):
        raise SystemExit("Image frontend differs from production build")
    (stage / "web-manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    compose = Path("deploy/compose.yml").read_text(encoding="utf-8")
    compose, replacements = re.subn(
        r"(?m)^    image:.*$", "    image: " + image, compose)
    if replacements != 1:
        raise SystemExit("Expected exactly one image in Compose")
    (stage / "compose.yml").write_text(compose, encoding="utf-8")
    build = {"repository": os.environ["GITHUB_REPOSITORY"], "commit": commit,
             "run_id": os.environ["GITHUB_RUN_ID"],
             "attempt": os.environ["GITHUB_RUN_ATTEMPT"], "image": image,
             "image_id": info["Id"], "rootfs_layers": info["RootFS"]["Layers"],
             "platform": "linux/amd64", "base_path": "/landlord/",
             "checks": ["frontend build", "date/PWA tests", "runtime and PDF smoke"],
             "automatic_deployment": False}
    (stage / "build.json").write_text(json.dumps(build, indent=2) + "\n", encoding="utf-8")
    subprocess.run(["git", "archive", "--format=tar.gz", "-o",
                    str(stage / "source.tar.gz"), commit], check=True)
    with tarfile.open(output / "release.tar.gz", "w:gz") as archive:
        for path in sorted(stage.iterdir()):
            archive.add(path, arcname=path.name)
    shutil.copyfile(stage / "build.json", output / "build.json")
with gzip.open(output / "image.tar.gz", "wb", compresslevel=6) as archive:
    producer = subprocess.Popen(["docker", "image", "save", image], stdout=subprocess.PIPE)
    try:
        shutil.copyfileobj(producer.stdout, archive)
    finally:
        producer.stdout.close()
    if producer.wait() != 0:
        raise SystemExit("Docker image export failed")
(output / "SHA256SUMS").write_text(
    "".join(sha(path) + "  " + path.name + "\n" for path in sorted(output.iterdir())),
    encoding="utf-8")
print("Packaged image, matching web, Compose, source, provenance and SHA256SUMS.")
