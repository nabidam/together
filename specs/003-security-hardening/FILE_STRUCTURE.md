# Security-hardening cycle file map

Existing files not listed here remain unchanged. This is the predicted delta map for `specs/003-security-hardening/`; the repository tree is authoritative during implementation.

```text
.
├── ARCHITECTURE.md                         # patch implemented threat/resource contracts
├── CONVENTIONS.md                          # patch security test and boundary conventions
├── README.md                               # document seed/upload configuration
├── cmd/
│   └── server/
│       └── main.go                         # fail-closed seed + upload maximum wiring
├── docs/
│   ├── HARDENING.md                        # operator controls and proxy trust
│   └── OPERATIONS.md                       # environment/config upgrade guidance
├── internal/
│   ├── auth/
│   │   ├── auth.go                         # seed policy + uniform login work
│   │   ├── auth_test.go
│   │   ├── http.go                         # invite entropy + limiter integration
│   │   ├── http_test.go
│   │   ├── throttle.go                     # new bounded token bucket/client-IP helper
│   │   └── throttle_test.go                # new fake-clock boundary tests
│   ├── live/
│   │   ├── hub.go                          # live connection cap + fixed-media start
│   │   ├── hub_test.go
│   │   ├── rooms.go                        # creation quotas/timer + token index
│   │   └── rooms_test.go
│   └── media/
│       ├── pipeline.go                     # preserve final output size contract
│       ├── upload.go                       # declared size and bounded bodies
│       └── upload_test.go
├── scripts/
│   ├── security-e2e.sh                     # new disposable production journey
│   └── verify.sh                           # integrate or explicitly skip journey
├── specs/
│   └── 003-security-hardening/
│       ├── SPEC.md
│       ├── PRD.md
│       ├── PLAN.md
│       ├── TASKS.md
│       ├── FILE_STRUCTURE.md
│       ├── evidence/
│       │   └── task-N.txt
│       ├── reviews/
│       │   └── REVIEW_N.md
│       └── screenshots/
└── web/
    └── src/
        └── lib/
            └── upload.js                   # send declared size and upload length
```
