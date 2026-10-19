"""COS release objects and a fail-closed publisher lock; no automatic retries."""

import base64
from dataclasses import dataclass
import datetime
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import uuid
import xml.etree.ElementTree as ET


class PublishError(Exception):
    pass


@dataclass
class COSResponse:
    status: int
    etag: str = None
    code: str = None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, *args):
        return None


class COSHTTP:
    def __init__(self, bucket, region, opener=None):
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*-[0-9]+", bucket) or not re.fullmatch(r"ap-[a-z]+(?:-[a-z]+)?", region):
            raise PublishError("Invalid COS bucket or region")
        self.host = f"{bucket}.cos.{region}.myqcloud.com"
        self.region = region
        self.opener = opener or urllib.request.build_opener(NoRedirect())

    def signed_url(self, key, method, headers):
        access, secret = os.environ.get("AWS_ACCESS_KEY_ID"), os.environ.get("AWS_SECRET_ACCESS_KEY")
        if not access or not secret:
            raise PublishError("COS publication requires credential environment variables")
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        day, path = stamp[:8], "/" + urllib.parse.quote(key, safe="/~")
        scope = f"{day}/{self.region}/s3/aws4_request"
        canonical = {"host": self.host, **{name.lower(): " ".join(value.split()) for name, value in headers.items()}}
        names = ";".join(sorted(canonical))
        header_text = "".join(name + ":" + canonical[name] + "\n" for name in sorted(canonical))
        query = {"X-Amz-Algorithm": "AWS4-HMAC-SHA256", "X-Amz-Credential": access + "/" + scope,
                 "X-Amz-Date": stamp, "X-Amz-Expires": "600", "X-Amz-SignedHeaders": names}
        if os.environ.get("AWS_SESSION_TOKEN"):
            query["X-Amz-Security-Token"] = os.environ["AWS_SESSION_TOKEN"]
        encode = lambda value: urllib.parse.quote(value, safe="~")
        query_text = "&".join(encode(name) + "=" + encode(value) for name, value in sorted(query.items()))
        request_text = f"{method}\n{path}\n{query_text}\n{header_text}\n{names}\nUNSIGNED-PAYLOAD"
        digest = lambda key, text: hmac.new(key, text.encode(), hashlib.sha256).digest()
        signing_key = digest(digest(digest(digest(("AWS4" + secret).encode(), day), self.region), "s3"), "aws4_request")
        to_sign = "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hashlib.sha256(request_text.encode()).hexdigest()
        signature = hmac.new(signing_key, to_sign.encode(), hashlib.sha256).hexdigest()
        return f"https://{self.host}{path}?{query_text}&X-Amz-Signature={signature}"

    def request(self, method, key, source=None, headers=None, destination=None, max_bytes=None):
        headers = dict(headers or {})
        stream = None
        try:
            body = source
            if isinstance(source, (Path, str)):
                stream = Path(source).open("rb")
                md5, size = hashlib.md5(usedforsecurity=False), 0
                while chunk := stream.read(1024 * 1024):
                    md5.update(chunk)
                    size += len(chunk)
                stream.seek(0)
                body = stream
            elif source is not None:
                size = len(source)
                md5 = hashlib.md5(source, usedforsecurity=False)
            if source is not None:
                headers.update({"Content-Length": str(size), "Content-MD5": base64.b64encode(md5.digest()).decode()})
            request = urllib.request.Request(self.signed_url(key, method, headers), data=body, method=method, headers=headers)
            # urllib sends this request once. In particular DELETE must never be
            # retried: another publisher may already own the same lock key.
            try:
                with self.opener.open(request, timeout=600) as response:
                    if destination is not None:
                        with Path(destination).open("wb") as output:
                            received = 0
                            while chunk := response.read(1024 * 1024):
                                received += len(chunk)
                                if max_bytes is not None and received > max_bytes:
                                    raise PublishError("COS object exceeds its read limit")
                                output.write(chunk)
                    else:
                        response.read(8192)
                    return COSResponse(response.status, response.headers.get("ETag"))
            except urllib.error.HTTPError as error:
                with error:
                    body = error.read(8192)
                    try:
                        code = ET.fromstring(body).findtext("Code")
                    except ET.ParseError:
                        code = None
                    return COSResponse(error.code, code=code)
        except (OSError, ValueError, http.client.HTTPException):
            # URL query signatures and credentials must never reach diagnostics.
            raise PublishError(f"COS {method} outcome is unknown") from None
        finally:
            if stream is not None:
                stream.close()


def fingerprint(path):
    digest, size = hashlib.sha256(), 0
    with Path(path).open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
            size += len(chunk)
    return size, digest.hexdigest()


class COSStore:
    probe_prefix = "beeftv/.publication-probes/"
    lock_prefix = "beeftv/.publication-locks/"
    claim_prefix = "beeftv/.publication-claims/"
    latest_key = "beeftv/desktop-update.json"

    def __init__(self, bucket, region, transport=None):
        self.http = transport or COSHTTP(bucket, region)
        self.bucket, self.region, self.provider = bucket, region, "cos"

    def get(self, key, destination, max_bytes=None):
        result = self.http.request("GET", key, destination=destination, max_bytes=max_bytes)
        if result.status == 404:
            return None
        if result.status != 200 or not result.etag:
            raise PublishError("Cannot read COS release object")
        return result.etag

    def create(self, key, source, cache_control):
        result = self.http.request("PUT", key, source, {"x-cos-forbid-overwrite": "true",
                                   "Cache-Control": cache_control, "Content-Type": self.content_type(key)})
        if result.status == 409 and result.code == "FileAlreadyExists":
            return False
        if result.status != 200:
            raise PublishError("COS immutable create was not confirmed")
        return True

    @staticmethod
    def content_type(key):
        return {".json": "application/json", ".zip": "application/zip", ".dmg": "application/x-apple-diskimage",
                ".exe": "application/vnd.microsoft.portable-executable", ".deb": "application/vnd.debian.binary-package"}.get(Path(key).suffix, "application/octet-stream")

    def put(self, key, source, cache_control, etag=None):
        if key == self.latest_key or etag is not None:
            return self.conditional_put(key, source, cache_control, etag)
        return self.create(key, source, cache_control)

    def lock_key(self, target):
        return self.lock_prefix + hashlib.sha256(target.encode()).hexdigest() + ".json"

    def claim_key(self, target, etag):
        # JSON null is the stable identity for an absent first feed. A quoted
        # ETag remains a distinct string. Never use a random claim key here.
        identity = json.dumps([target, etag], separators=(",", ":"), ensure_ascii=True).encode()
        prefix = self.probe_prefix + "claims/" if target.startswith(self.probe_prefix) else self.claim_prefix
        return prefix + hashlib.sha256(identity).hexdigest() + ".json"

    def lock_bytes(self, target):
        owner = {name: os.environ[name][:256] for name in ("GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB") if os.environ.get(name)}
        return json.dumps({"schema": 1, "nonce": uuid.uuid4().hex, "target": target, "owner": owner,
                           "createdAt": datetime.datetime.now(datetime.timezone.utc).isoformat()}, sort_keys=True).encode()

    def _lock_content(self, key):
        with tempfile.TemporaryDirectory(prefix="beeftv-lock-read-") as directory:
            path = Path(directory) / "lock"
            return None if self.get(key, path, max_bytes=8192) is None else path.read_bytes()

    def acquire(self, target):
        key, content = self.lock_key(target), self.lock_bytes(target)
        uncertain = False
        try:
            self.create(key, content, "no-store, max-age=0")
        except PublishError:
            # A single GET of the exact nonce reconciles a lost create receipt.
            # Never retry creation or invent another nonce after an unknown PUT.
            uncertain = True
        if self._lock_content(key) != content:
            if uncertain:
                raise PublishError("COS publisher lock acquisition is unconfirmed; publication is blocked")
            raise PublishError("COS publisher lock is already held; publication is blocked")
        return key, content

    def release(self, lock):
        key, content = lock
        if self._lock_content(key) != content:
            raise PublishError("COS publisher lock ownership changed; refusing to delete it")
        try:
            result = self.http.request("DELETE", key)
            if result.status in (200, 204):
                return
        except PublishError:
            pass
        # A lost DELETE receipt may already have admitted a new writer. Only
        # read the key now; never send another DELETE for this release attempt.
        if self._lock_content(key) == content:
            raise PublishError("COS publisher lock release is unconfirmed; operator recovery is required")

    def consume_etag(self, target, etag, expected, lock):
        key = self.claim_key(target, etag)
        document = json.loads(lock[1])
        content = json.dumps({"schema": 1, "target": target, "expectedETag": etag,
                              "nextSize": expected[0], "nextSHA256": expected[1],
                              "publisher": document}, sort_keys=True).encode()
        try:
            created = self.create(key, content, "no-store, max-age=0")
        except PublishError:
            if self._lock_content(key) != content:
                raise PublishError("COS ETag claim is unconfirmed; publisher lock retained for operator recovery") from None
            return True
        if not created:
            return False
        if self._lock_content(key) != content:
            raise PublishError("COS ETag claim readback failed; publisher lock retained for operator recovery")
        return True

    def conditional_put(self, key, source, cache_control, etag):
        expected = fingerprint(source)
        lock = self.acquire(key)
        may_release = True
        try:
            with tempfile.TemporaryDirectory(prefix="beeftv-cos-cas-") as directory:
                current = Path(directory) / "current"
                actual = self.get(key, current)
                if actual != etag:
                    return False
                if self._lock_content(lock[0]) != lock[1]:
                    raise PublishError("COS publisher lock ownership changed before write")
                # An immutable claim consumes each expected ETag at most once.
                # This also rejects a stale origin read after an earlier owner
                # published and released the lock; no strong GET assumption.
                may_release = False
                if not self.consume_etag(key, etag, expected, lock):
                    may_release = True
                    return False
                # A claim/target transport failure or incomplete readback keeps
                # the lock. A later writer cannot overtake an unknown mutation.
                try:
                    result = self.http.request("PUT", key, source, {"Cache-Control": cache_control,
                                                                   "Content-Type": self.content_type(key)})
                except PublishError:
                    raise PublishError("COS feed PUT outcome is unknown; publisher lock and ETag claim retained") from None
                if result.status != 200 or self.get(key, current) is None or fingerprint(current) != expected:
                    raise PublishError("COS feed write is unverified; publisher lock retained for operator recovery")
                may_release = True
                return True
        finally:
            if may_release:
                self.release(lock)

    def delete_probe(self, key):
        if not key.startswith(self.probe_prefix):
            raise PublishError("Refusing to delete a release object")
        try:
            result = self.http.request("DELETE", key)
            if result.status in (200, 204):
                return
        except PublishError:
            pass
        if self._lock_content(key) is not None:
            raise PublishError("COS probe cleanup is unconfirmed")

    def check_conditions(self):
        key = self.probe_prefix + uuid.uuid4().hex + "/object"
        with tempfile.TemporaryDirectory(prefix="beeftv-cos-probe-") as directory:
            first, second, current = (Path(directory) / name for name in ("first", "second", "current"))
            first.write_bytes(b"COS immutable-create probe A")
            second.write_bytes(b"COS immutable-create probe B")
            try:
                if not self.create(key, first, "no-store") or self.create(key, second, "no-store"):
                    raise PublishError("COS forbid-overwrite is ineffective; publication is blocked")
                if self.get(key, current) is None or fingerprint(current) != fingerprint(first):
                    raise PublishError("COS forbid-overwrite changed existing bytes; publication is blocked")
            finally:
                self.delete_probe(key)
