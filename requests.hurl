# Fast echo — simple insert, should succeed cleanly
POST http://localhost:8080/v1/echo
Content-Type: application/json
X-Demo-Label: fast-echo-test
{
    "message": "fast echo test"
}
HTTP 200
[Asserts]
jsonpath "$.message" == "echo: fast echo test"


# Slow echo — transaction with pg_sleep(2), should still succeed, just slowly
POST http://localhost:8080/v1/echo/slow
Content-Type: application/json
X-Demo-Label: slow-echo-test
{
    "message": "slow echo test"
}
HTTP 200
[Asserts]
jsonpath "$.message" == "echo: slow echo test"
duration >= 2000


# Fail echo — second insert fails, should return an error status
POST http://localhost:8080/v1/echo/fail
Content-Type: application/json
X-Demo-Label: fail-echo-test
{
    "message": "fail echo test"
}
HTTP 500


# Double tx echo — the bug case, should still succeed functionally
# (the bug is about connection usage, not correctness of the response)
POST http://localhost:8080/v1/echo/doubletx
Content-Type: application/json
X-Demo-Label: double-tx-test
{
    "message": "double tx test"
}
HTTP 200
[Asserts]
jsonpath "$.message" == "echo: double tx test"


# Fixed tx echo — same logical result as doubletx, but via one transaction
POST http://localhost:8080/v1/echo/fixedtx
Content-Type: application/json
X-Demo-Label: fixed-tx-test
{
    "message": "fixed tx test"
}
HTTP 200
[Asserts]
jsonpath "$.message" == "echo: fixed tx test"


# Empty message — should hit the validation error path
POST http://localhost:8080/v1/echo
Content-Type: application/json
X-Demo-Label: empty-message
{
    "message": ""
}
HTTP 500