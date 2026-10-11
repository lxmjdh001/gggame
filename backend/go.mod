module starlight/backend

go 1.25.0

require (
	go.etcd.io/bbolt v1.4.3
	starlight/gameapi v0.0.0
)

require golang.org/x/sys v0.29.0 // indirect

replace starlight/gameapi => ../game-api
