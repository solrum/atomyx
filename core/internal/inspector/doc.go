// Package inspector serves the browser inspector of a held device: a
// loopback HTTP server that shows what the holder has captured and,
// for the operator, asks the holder for more.
//
// The server reaches the device only through a session.Controller, the
// read and fresh-capture port of a session. It never takes the built
// driver or the Orchestra, so every device operation it triggers goes
// through the session's activity gate.
//
// Trust boundary. The listener binds 127.0.0.1 with a system-chosen
// port and nothing else. Every request must name that listener in its
// Host header, which defeats DNS rebinding. Every request under /api/
// must carry the session token in X-Atomyx-Token, a GET and the event
// stream included, so a cross-origin navigation, an image tag or a link
// can trigger no read. Every non-GET request under /api/ must also
// carry an Origin equal to the served origin. Only GET / is exempt from
// the token, because it delivers the page that performs the exchange.
//
// Bootstrap. A launcher mints a single-use code with
// Server.NewBootstrapURL and hands the URL to the operator. The code
// travels in the URL fragment, which a browser never sends to a
// server. The page posts it to /api/v1/auth, receives a token, and
// keeps the token in memory only. Every exchange is logged, so a code
// read from another account's process arguments is visible when used.
//
// Routes are added through route groups, so a feature adds a file with
// its own group and never edits the server.
package inspector
