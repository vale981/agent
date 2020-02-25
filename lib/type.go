package lib

//{"id": 1, "name": "Simulators", "port": 7624, "autostart": 0, "autoconnect": 0}
type INDIProfile struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Port        uint32 `json:"port"`
	AutoStart   uint32 `json:"autostart"`
	AutoConnect uint32 `json:"autoconnect"`
}

/*
[
	{"binary": "indi_asi_ccd", "skeleton": null, "family": "CCDs", "label": "ZWO CCD", "version": "1.5", "role": "", "custom": false, "name": "ZWO CCD"},
	{"binary": "indi_ieq_telescope", "skeleton": null, "family": "Telescopes", "label": "iOptron CEM25", "version": "1.8", "role": "", "custom": false, "name": "iEQ"},
	{"binary": "indi_asi_focuser", "skeleton": null, "family": "Focusers", "label": "ASI EAF", "version": "1.5", "role": "", "custom": false, "name": "ASI EAF"}
]
*/
type INDIDriver struct {
	Binary   string      `json:"binary"`
	Skeleton interface{} `json:"skeleton"`
	Family   string      `json:"family"`
	Label    string      `json:"label"`
	Version  string      `json:"version"`
	Role     string      `json:"role"`
	Custom   bool        `json:"custom"`
	Name     string      `json:"name"`
}
