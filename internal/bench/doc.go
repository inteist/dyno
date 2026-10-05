// Package bench runs one benchmark session: the schedule, the model
// lifecycle, the cache preparation, the measured runs, and the events. It
// depends only on ports, so tests run without a server, a database, or a
// real clock.
package bench
