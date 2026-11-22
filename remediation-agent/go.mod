module github.com/smhussa22/aero-agent/remediation-agent

go 1.22

require (
	github.com/smhussa22/aero-agent/backend v0.0.0
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.34.2
)

require (
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
)

replace github.com/smhussa22/aero-agent/backend => ../backend
