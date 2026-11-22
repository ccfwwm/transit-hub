#!/usr/bin/env python3
import fcntl
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path("/opt/transit-hub")
COMPOSE = ROOT / "deploy/docker-compose.subcheck.yml"
DOCKER_COMPOSE = ["docker", "compose", "--env-file", str(ROOT / ".env.production"), "-f", str(COMPOSE)]
HEALTH = "http://127.0.0.1:10621/api/health"
VERSION = re.compile(r"[0-9]{1,8}\.[0-9]{1,8}\.[0-9]{1,8}")
APP_IMAGE = re.compile(r"(?m)(^  app:\n    image: )(transithub:\S+)$")


def run(args, timeout=60):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, text=True, timeout=timeout).stdout.strip()


def app():
    container = run([*DOCKER_COMPOSE, "ps", "-q", "app"])
    if not container:
        raise RuntimeError("TransitHub app is not running")
    live = json.loads(run(["docker", "inspect", container]))[0]
    env = dict(v.split("=", 1) for v in live["Config"]["Env"])
    version = env.get("APP_VERSION", "")
    if not VERSION.fullmatch(version):
        raise RuntimeError("Production app version is unavailable")
    return live["Image"], version


def check_health(expected_image, attempts=1):
    for attempt in range(attempts):
        try:
            with urllib.request.urlopen(HEALTH, timeout=3) as response:
                if json.load(response).get("status") == "ok" and app()[0] == expected_image:
                    return
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
            pass
        if attempt + 1 < attempts:
            time.sleep(2)
    raise RuntimeError("TransitHub failed its health check")


def receive_image(path, checksum):
    digest = hashlib.sha256()
    size = 0
    with path.open("xb") as archive:
        for data in iter(lambda: sys.stdin.buffer.read(1024 * 1024), b""):
            size += len(data)
            if size > 768 * 1024 * 1024:
                raise ValueError("Image archive exceeds 768 MiB")
            digest.update(data)
            archive.write(data)
    if not size or digest.hexdigest() != checksum:
        raise ValueError("Image archive checksum mismatch")


def save_compose(text):
    with tempfile.NamedTemporaryFile("w", dir=COMPOSE.parent, delete=False) as file:
        file.write(text)
        file.flush()
        os.fsync(file.fileno())
    os.replace(file.name, COMPOSE)


def restart_app():
    run([*DOCKER_COMPOSE, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "app"])


def deploy(revision, version, checksum):
    old_image, old_version = app()
    if tuple(map(int, version.split("."))) < tuple(map(int, old_version.split("."))):
        raise ValueError(f"Release {version} is older than production {old_version}")
    image_tag = f"transithub:gha-{revision}"
    compose = COMPOSE.read_text()
    updated, matches = APP_IMAGE.subn(lambda match: match[1] + image_tag, compose)
    if matches != 1:
        raise RuntimeError("Expected one production app image")
    with tempfile.TemporaryDirectory(prefix="transithub-image-", dir=ROOT) as work:
        archive = Path(work) / "image.tar.gz"
        receive_image(archive, checksum)
        run(["docker", "image", "load", "-i", str(archive)], timeout=240)
    image = json.loads(run(["docker", "image", "inspect", image_tag]))[0]
    labels = image["Config"].get("Labels") or {}
    if (image["Os"], image["Architecture"], labels.get("org.opencontainers.image.revision"),
        labels.get("org.opencontainers.image.version")) != ("linux", "amd64", revision, version):
        raise ValueError("Image metadata does not match the release")
    if f"APP_VERSION={version}" not in (image["Config"].get("Env") or []):
        raise ValueError("Image APP_VERSION does not match the release")

    try:
        save_compose(updated)
        restart_app()
        check_health(image["Id"], attempts=30)
    except BaseException:
        save_compose(compose)
        restart_app()
        check_health(old_image, attempts=30)
        raise


def interrupted(signum, frame):
    raise RuntimeError(f"Deployment interrupted by signal {signum}")


def main():
    args = os.environ.get("SSH_ORIGINAL_COMMAND", "").split()
    if args == ["verify"]:
        image, version = app()
        check_health(image)
    else:
        if (len(args) != 4 or args[0] != "deploy" or not re.fullmatch(r"[0-9a-f]{40}", args[1])
            or not VERSION.fullmatch(args[2]) or not re.fullmatch(r"[0-9a-f]{64}", args[3])):
            raise ValueError("Expected verify or deploy REVISION VERSION SHA256")
        signal.signal(signal.SIGHUP, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
        with (ROOT / "deploy/.github-deploy.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            deploy(*args[1:])
        version = args[2]
    print(json.dumps({"service": "TransitHub", "status": "ok", "version": version}))


if __name__ == "__main__":
    os.umask(0o077)
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
