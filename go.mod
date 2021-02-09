module github.com/bruno-anjos/archimedesHTTPClient

go 1.14

require (
	github.com/bruno-anjos/cloud-edge-deployment v0.0.1
	github.com/docker/go-connections v0.4.0
	github.com/golang/geo v0.0.0-20200730024412-e86565bf3f35
	github.com/google/uuid v1.1.2
	github.com/sirupsen/logrus v1.7.0
)

replace github.com/bruno-anjos/cloud-edge-deployment v0.0.1 => ../cloud-edge-deployment
