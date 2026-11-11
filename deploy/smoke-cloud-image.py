"""Check the built image in a disposable container, without production access."""
import json
import os
import subprocess
import time
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


image = os.environ["LANDLORD_IMAGE"]
version = os.environ["LANDLORD_VERSION"]
name = "landlord-ci-" + uuid.uuid4().hex[:12]
try:
    docker(
        "run", "-d", "--name", name, "--read-only", "--cap-drop=ALL",
        "--security-opt=no-new-privileges", "--memory=512m", "--memory-swap=768m",
        "--cpus=1", "--pids-limit=160", "-e", "GOMEMLIMIT=192MiB",
        "--tmpfs", "/tmp:rw,size=256m,mode=1777",
        "--tmpfs", "/app/data:rw,size=32m,uid=10001,gid=10001,mode=0700",
        "-p", "127.0.0.1::8910", image,
    )
    address = docker("port", name, "8910/tcp")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for attempt in range(30):
        try:
            with opener.open("http://" + address + "/api/health", timeout=2) as response:
                health = json.load(response)
            break
        except (OSError, ValueError):
            if attempt == 29:
                raise
            time.sleep(1)
    assert health["code"] == 0 and health["data"]["version"] == version, health
    with opener.open("http://" + address + "/", timeout=5) as response:
        assert '/landlord/assets/' in response.read().decode(), "Wrong frontend base"
    engine = docker("exec", name, "/usr/bin/chromium-headless-shell", "--version")
    docker("exec", name, "/bin/sh", "-ec", "test ! -e /usr/bin/chromium")
    # Use the same local-file input and Chromium arguments as chromiumPDF.
    subprocess.run(
        ["docker", "exec", "-i", name, "/bin/sh", "-ec",
         "mkdir -p /tmp/ci-pdf; cat > /tmp/ci-pdf/document.html"],
        input='<meta charset="UTF-8"><p>Landlord 合同与收据</p>', text=True, check=True)
    docker(
        "exec", name, "/usr/local/bin/landlord-chromium", "--headless=new",
        "--disable-gpu", "--no-first-run", "--no-default-browser-check",
        "--disable-extensions", "--disable-background-networking",
        "--no-pdf-header-footer", "--user-data-dir=/tmp/ci-pdf/profile",
        "--print-to-pdf=/tmp/ci-pdf/document.pdf", "file:///tmp/ci-pdf/document.html",
    )
    docker("exec", name, "/bin/bash", "-ec",
           "test $(stat -c %s /tmp/ci-pdf/document.pdf) -gt 1000; "
           "test \"$(head -c 5 /tmp/ci-pdf/document.pdf)\" = '%PDF-'; rm -rf /tmp/ci-pdf")
    print(json.dumps({"runtime": "PASS", "version": version,
                      "frontend_base": "/landlord/", "pdf": "PASS", "engine": engine,
                      "full_browser_absent": True, "business_renderer_arguments": True}))
finally:
    subprocess.run(["docker", "logs", "--tail=30", name], check=False)
    subprocess.run(["docker", "rm", "-f", name], check=False)
