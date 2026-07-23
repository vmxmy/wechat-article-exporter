// Package web is the loopback-only browser presentation adapter.
//
// It may depend on application.Application for presentation-safe status and
// future use cases. It must not reach into profile storage, object storage,
// WeChat protocol, job scheduling, or secret persistence directly. The Cobra
// composition root supplies the selected profile's application seam.
package web
