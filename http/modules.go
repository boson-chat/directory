package http

// Every feature module registers itself (router.Register in an init
// function). Importing the package here is what turns it on.
import (
	_ "github.com/boson-chat/directory/http/handlers"            // core: public, owner, operator, legacy API + verify/health workers
	_ "github.com/boson-chat/directory/http/handlers/federation" // feed, peers API, sync worker, built-in main peer
)
