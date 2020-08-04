module github.com/bruno-anjos/archimedesHTTPClient

go 1.13

require (
	github.com/bruno-anjos/archimedes v0.0.0-20200731153328-0fb213b05ee7
	github.com/bruno-anjos/solution-utils v0.0.0-20200804140242-989a419bda22
	github.com/docker/go-connections v0.4.0
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
)

replace (
	github.com/bruno-anjos/archimedes v0.0.0-20200731153328-0fb213b05ee7 => ./../archimedes
	github.com/bruno-anjos/solution-utils v0.0.0-20200731153528-f4f5b5285b7d => ./../solution-utils
)
