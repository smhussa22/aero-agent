module github.com/smhussa22/aero-agent/remediation-agent

go 1.26.11

require (
	github.com/smhussa22/aero-agent/backend v0.0.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261118221215-c8830f628b3f // indirect
)

replace github.com/smhussa22/aero-agent/backend => ../backend
