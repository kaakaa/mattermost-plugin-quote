module github.com/kaakaa/mattermost-plugin-share-post

go 1.12

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/gorilla/mux v1.7.3
	github.com/mattermost/mattermost-server/v5 v5.22.1
	github.com/mholt/archiver/v3 v3.3.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.4.0
)

// FIXME: Remove this line
replace github.com/kaakaa/mattermost-plugin-share-post => ./
