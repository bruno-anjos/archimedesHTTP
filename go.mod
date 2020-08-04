module github.com/bruno-anjos/archimedesHTTPClient

go 1.13

require (
	github.com/bruno-anjos/archimedes v0.0.1
	github.com/bruno-anjos/solution-utils v0.0.1
	github.com/docker/go-connections v0.4.0
	github.com/sirupsen/logrus v1.6.0
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
)

replace (
	github.com/bruno-anjos/archimedes v0.0.1 => ./../archimedes
	github.com/bruno-anjos/solution-utils v0.0.1 => ./../solution-utils
)
