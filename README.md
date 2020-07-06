# A simple transmission bandwidth trottling module

## Usage

This module provides a method to trottle an outgoing traffic from a service.
After initialization, using the `NewTransmissionBandwidthTrottle method,
simply wrap any connection with `TrottleConnction` method.

```
conn, err := l.Accept()
if err != nil {
    panic(err)
}
conn = trottle.TrottleConnction(conn)
server(conn)

```

`SetServerBandwidthLimit` method updates the total server bandwidth limit
`SetConnectionBandwidthLimit` method updates the bandwidth limits for all existing connections

## Testing

Functional tests are provided in the `tests` directory of the project.
Run `go test` to execute.
