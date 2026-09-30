package core

// LocalTZID is the IANA name of the system time zone ("" if unknown). New events are
// stored in it so that repeating events keep their local time across DST changes.
var LocalTZID = detectLocalTZID()
