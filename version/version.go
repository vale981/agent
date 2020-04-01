package version

import (
	"log"

	"github.com/fatih/color"
)

var AgentVersion = "1.0.5"

func CheckAgentVersion(latestVer string) {
	log.Println("Current agent version:", AgentVersion)

	if AgentVersion < latestVer {
		log.Println("Latest agent version:", latestVer)
		yc := color.New(color.FgYellow)
		yc.Println()
		yc.Println("                                ************************************************************")
		yc.Println("                                *          WARNING: you version of agent is outdated!      *")
		yc.Println("                                *                                                          *")
		yc.Println("                                *          Please download the latest version from:        *")
		yc.Println("                                *          https://indihub.space/downloads                 *")
		yc.Println("                                *                                                          *")
		yc.Println("                                ************************************************************")
		yc.Println("                                                                                            ")
	}
}
