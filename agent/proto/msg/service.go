package msg

/*
 * Second-level message:
 *   1. proto.Msg{Cmd:"start-service", Data: "..."}
 *                                       |
 *                                       V
 *   2. StartService{Name:"query-history", Config: "..."}
 *
 * The Msg handler for "start-service" in agent.go will create a StartService
 * struct and pass StartService.Config it to the Start() method for the service
 * manager for StartService.Name, e.g. QhManager.Start(), which will decode
 * the Config.
 */
type StartService struct {
	Name string
	Config []byte
}

type StopService struct {
	Name string
}

type UpdateService struct {
	Name string
	Config []byte
}
