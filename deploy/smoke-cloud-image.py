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
    docker(
        "exec", name, "/usr/local/bin/landlord-chromium", "--headless",
        "--disable-gpu", "--user-data-dir=/tmp/ci-pdf-profile",
        "--print-to-pdf=/tmp/ci.pdf", "--no-pdf-header-footer",
        'data:text/html,<meta charset="UTF-8"><p>Landlord 合同与收据</p>',
    )
    docker("exec", name, "/bin/bash", "-ec",
           "test $(stat -c %s /tmp/ci.pdf) -gt 1000; "
           "test \"$(head -c 5 /tmp/ci.pdf)\" = '%PDF-'")
    print(json.dumps({"runtime": "PASS", "version": version,
                      "frontend_base": "/landlord/", "pdf": "PASS"}))
finally:
    subprocess.run(["docker", "logs", "--tail=30", name], check=False)
    subprocess.run(["docker", "rm", "-f", name], check=False)
