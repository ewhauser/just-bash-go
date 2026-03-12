module github.com/ewhauser/gbash/contrib/jq

go 1.26.0

require (
	github.com/ewhauser/gbash v0.0.0
	github.com/itchyny/gojq v0.12.18
)

require (
	github.com/benhoyt/goawk v1.31.0 // indirect
	github.com/itchyny/timefmt-go v0.1.7 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/term v0.40.0 // indirect
	mvdan.cc/sh/v3 v3.13.0 // indirect
)

replace github.com/ewhauser/gbash => ../..
