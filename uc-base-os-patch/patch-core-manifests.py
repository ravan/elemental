#!/usr/bin/env python3

import argparse
import io
import json
import os
import tarfile
import urllib.parse
import urllib.request

import yaml


REGISTRY_AUTH_REALM = "https://scc.suse.com/api/registry/authorize"
REGISTRY_AUTH_SERVICE = "SUSE Linux Docker Registry"
OCI_ACCEPT = ", ".join(
    [
        "application/vnd.oci.image.index.v1+json",
        "application/vnd.docker.distribution.manifest.list.v2+json",
        "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.docker.distribution.manifest.v2+json",
    ]
)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-repo", required=True)
    parser.add_argument("--target-os-image", required=True)
    parser.add_argument("--target-iso-image", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("tags", nargs="+")
    args = parser.parse_args()

    token = registry_token(args.source_repo)
    os.makedirs(args.output_dir, exist_ok=True)

    for tag in args.tags:
        manifest = image_manifest(args.source_repo, tag, token)
        data = release_manifest(args.source_repo, manifest, token)
        patched, removed_elemental3ctl_sysext = patch_release_manifest(
            data, args.target_os_image, args.target_iso_image
        )
        outdir = os.path.join(args.output_dir, tag)
        os.makedirs(outdir, exist_ok=True)
        with open(os.path.join(outdir, "release_manifest.yaml"), "w", encoding="utf-8") as f:
            f.write(patched)
        with open(os.path.join(outdir, "Dockerfile"), "w", encoding="utf-8") as f:
            f.write("FROM scratch\nCOPY release_manifest.yaml /release_manifest.yaml\n")
        message = (
            f"{tag}: operatingSystem.image.base -> {args.target_os_image}; "
            f"operatingSystem.image.iso -> {args.target_iso_image}"
        )
        if removed_elemental3ctl_sysext:
            message += "; removed obsolete elemental3ctl systemd extension"
        print(message)


def registry_token(repo: str) -> str:
    scope = f"repository:{repo}:pull"
    url = (
        f"{REGISTRY_AUTH_REALM}?service={urllib.parse.quote(REGISTRY_AUTH_SERVICE)}"
        f"&scope={urllib.parse.quote(scope)}"
    )
    with urllib.request.urlopen(url) as response:
        return json.load(response)["token"]


def image_manifest(repo: str, ref: str, token: str) -> dict:
    top = registry_manifest(repo, ref, token)
    if "manifests" not in top:
        return top
    linux_amd64 = next(
        (
            m
            for m in top["manifests"]
            if m.get("platform", {}).get("os") == "linux"
            and m.get("platform", {}).get("architecture") == "amd64"
        ),
        top["manifests"][0],
    )
    return registry_manifest(repo, linux_amd64["digest"], token)


def registry_manifest(repo: str, ref: str, token: str) -> dict:
    request = urllib.request.Request(
        f"https://registry.suse.com/v2/{repo}/manifests/{ref}",
        headers={"Authorization": f"Bearer {token}", "Accept": OCI_ACCEPT},
    )
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def release_manifest(repo: str, manifest: dict, token: str) -> str:
    for layer in manifest.get("layers", []):
        request = urllib.request.Request(
            f"https://registry.suse.com/v2/{repo}/blobs/{layer['digest']}",
            headers={"Authorization": f"Bearer {token}"},
        )
        with urllib.request.urlopen(request) as response:
            blob = response.read()
        if str(layer.get("mediaType", "")).endswith("+zstd"):
            import zstandard

            with zstandard.ZstdDecompressor().stream_reader(io.BytesIO(blob)) as reader:
                blob = reader.read()
        with tarfile.open(fileobj=io.BytesIO(blob), mode="r:*") as archive:
            for member in archive.getmembers():
                if os.path.basename(member.name) != "release_manifest.yaml":
                    continue
                extracted = archive.extractfile(member)
                if extracted is None:
                    continue
                return extracted.read().decode("utf-8")
    raise RuntimeError("release_manifest.yaml not found")


def patch_release_manifest(data: str, target_os_image: str, target_iso_image: str) -> tuple[str, bool]:
    manifest = yaml.safe_load(data)
    if not isinstance(manifest, dict):
        raise RuntimeError("release_manifest.yaml is not a YAML mapping")

    try:
        image = manifest["components"]["operatingSystem"]["image"]
        image["base"] = target_os_image
        image["iso"] = target_iso_image
    except KeyError as err:
        raise RuntimeError("components.operatingSystem.image.base or .iso not found") from err

    removed_elemental3ctl_sysext = remove_elemental3ctl_sysext(manifest)
    return yaml.safe_dump(manifest, sort_keys=False), removed_elemental3ctl_sysext


def remove_elemental3ctl_sysext(manifest: dict) -> bool:
    systemd = manifest.get("components", {}).get("systemd")
    if not isinstance(systemd, dict):
        return False

    extensions = systemd.get("extensions")
    if not isinstance(extensions, list):
        return False

    filtered = [
        extension
        for extension in extensions
        if not (isinstance(extension, dict) and extension.get("name") == "elemental3ctl")
    ]
    if len(filtered) == len(extensions):
        return False

    if filtered:
        systemd["extensions"] = filtered
    else:
        del systemd["extensions"]
        if not systemd:
            del manifest["components"]["systemd"]

    return True


if __name__ == "__main__":
    main()
