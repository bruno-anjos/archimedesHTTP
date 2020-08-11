module github.com/bruno-anjos/archimedesHTTPClient

go 1.13

require (
	github.com/bruno-anjos/archimedes v0.0.2
	github.com/bruno-anjos/solution-utils v0.0.1
	github.com/docker/go-connections v0.4.0
	github.com/sirupsen/logrus v1.6.0
)

replace (
	github.com/bruno-anjos/archimedes v0.0.2 => ./../archimedes
	github.com/bruno-anjos/solution-utils v0.0.1 => ./../solution-utils
)
