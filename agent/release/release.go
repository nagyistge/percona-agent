// Agent version is a property of the whole agent and is imported from several places,
// we need this package to avoid import cycles
package release

// REV="$(git rev-parse HEAD)"
// go build -ldflags "-X github.com/percona/percon-agent/agent.REVISION $REV"
var REVISION string = "0"
var VERSION string = "1.1.0"
var REL string = ""
