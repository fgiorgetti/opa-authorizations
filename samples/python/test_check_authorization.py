import io
import json
import unittest
import urllib.error
from unittest import mock

import check_authorization as sample


def fake_response(payload, status=200):
    response = io.BytesIO(json.dumps(payload).encode("utf-8"))
    response.status = status
    return response


def answers(*values):
    it = iter(values)
    return lambda _prompt: next(it)


class CollectInputTest(unittest.TestCase):
    def test_collects_values(self):
        body = sample.collect_input(answers("team-a", "Connector", "orders"))
        self.assertEqual(
            body,
            {"namespace": "team-a", "type": "connector", "routing_key": "orders"},
        )

    def test_asks_again_on_empty_and_invalid_values(self):
        with mock.patch("builtins.print"):
            body = sample.collect_input(
                answers("", "team-a", "router", "listener", " ", "inventory")
            )
        self.assertEqual(body["namespace"], "team-a")
        self.assertEqual(body["type"], "listener")
        self.assertEqual(body["routing_key"], "inventory")


class BaseUrlTest(unittest.TestCase):
    def test_default(self):
        self.assertEqual(sample.base_url({}), "http://127.0.0.1:8080")

    def test_strips_trailing_slash(self):
        self.assertEqual(
            sample.base_url({"OPA_AUTHZ_URL": "https://host:8443/"}),
            "https://host:8443",
        )

    def test_rejects_other_schemes(self):
        for value in ("file:///etc/passwd", "ftp://host", "host:8080", "http://"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    sample.base_url({"OPA_AUTHZ_URL": value})


class CallApiTest(unittest.TestCase):
    def test_sends_expected_request(self):
        body = {"namespace": "ns1", "type": "connector", "routing_key": "svc.a"}
        with mock.patch(
            "urllib.request.urlopen",
            return_value=fake_response({"allowed": True, "violations": []}),
        ) as urlopen:
            result = sample.call_api("http://127.0.0.1:8080", body)

        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "http://127.0.0.1:8080/api/authorize")
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(json.loads(request.data), body)
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(urlopen.call_args.kwargs["timeout"], sample.TIMEOUT_SECONDS)
        self.assertTrue(result["allowed"])

    def test_rejects_bad_response_format(self):
        with mock.patch("urllib.request.urlopen", return_value=fake_response([1])):
            with self.assertRaises(RuntimeError):
                sample.call_api("http://127.0.0.1:8080", {})


class FormatResultTest(unittest.TestCase):
    def test_allowed(self):
        self.assertEqual(
            sample.format_result({"allowed": True, "violations": []}), ["ALLOWED"]
        )

    def test_denied_lists_violations(self):
        lines = sample.format_result({"allowed": False, "violations": ["a", "b"]})
        self.assertEqual(lines, ["DENIED", "  - a", "  - b"])


class MainTest(unittest.TestCase):
    def run_main(self, urlopen_result=None, urlopen_error=None, env=None):
        patches = [
            mock.patch.dict("os.environ", env or {}, clear=True),
            mock.patch("check_authorization.collect_input", return_value={"k": "v"}),
            mock.patch("sys.stdout", new_callable=io.StringIO),
            mock.patch("sys.stderr", new_callable=io.StringIO),
        ]
        if urlopen_error is not None:
            patches.append(mock.patch("urllib.request.urlopen", side_effect=urlopen_error))
        else:
            patches.append(mock.patch("urllib.request.urlopen", return_value=urlopen_result))

        with patches[0], patches[1], patches[4]:
            with patches[2] as out, patches[3] as err:
                code = sample.main()
        return code, out.getvalue(), err.getvalue()

    def test_allowed_exits_zero(self):
        code, out, _ = self.run_main(fake_response({"allowed": True, "violations": []}))
        self.assertEqual(code, 0)
        self.assertEqual(out.strip(), "ALLOWED")

    def test_denied_exits_three_and_prints_violations(self):
        code, out, _ = self.run_main(fake_response({"allowed": False, "violations": ["nope"]}))
        self.assertEqual(code, 3)
        self.assertIn("DENIED", out)
        self.assertIn("nope", out)

    def test_http_error(self):
        error = urllib.error.HTTPError("http://x", 400, "Bad Request", {}, None)
        code, _, err = self.run_main(urlopen_error=error)
        self.assertEqual(code, 1)
        self.assertIn("status 400", err)

    def test_connection_error(self):
        code, _, err = self.run_main(urlopen_error=urllib.error.URLError("refused"))
        self.assertEqual(code, 1)
        self.assertIn("cannot reach", err)
        self.assertNotIn("Traceback", err)

    def test_timeout(self):
        code, _, err = self.run_main(urlopen_error=TimeoutError())
        self.assertEqual(code, 1)
        self.assertIn("cannot reach", err)

    def test_invalid_url(self):
        code, _, err = self.run_main(env={"OPA_AUTHZ_URL": "ftp://host"})
        self.assertEqual(code, 2)
        self.assertIn("OPA_AUTHZ_URL", err)


if __name__ == "__main__":
    unittest.main()
