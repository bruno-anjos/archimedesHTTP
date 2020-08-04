module github.com/bruno-anjos/archimedesHTTPClient

go 1.13

require (
	github.com/bruno-anjos/archimedes v0.0.0-20200804140801-448f3fb725ac
	github.com/bruno-anjos/solution-utils v0.0.0-20200804140242-989a419bda22
	github.com/docker/go-connections v0.4.0
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
)

replace (
	github.com/bruno-anjos/archimedes v0.0.0-20200804140801-448f3fb725ac => ./../archimedes
	github.com/bruno-anjos/solution-utils v0.0.0-20200804140242-989a419bda22 => ./../solution-utils
)
