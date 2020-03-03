# indihub-agent

The `indihub-agent` is a command line interface tool to connect your astro-photography equipment to [INDIHUB](https://indihub.space) network. The network can be used to share you equipment with others, use others' equipment remotely or just use your equipment without sharing but still contribute your images to INDIHUB-network.

NOTE: all astro-photos taken via INDIHUB-network (with auto-guiding or main imaging cameras) will be processed by INDIHUB cloud pipeline and used for scientific purposes.

## Prerequisites

0. You have a desire to contribute to Space exploration and sustainability projects.
1. You have motorized astro-photography equipment connected to your home network.
2. Your equipment is controlled by [INDI-server](https://github.com/indilib/indi), manuals and docs can be found on [INDI-lib](http://indilib.org) Web-site.
3. INDI-server is controlled by [INDI Web Manager](https://github.com/knro/indiwebmanager).
4. You have ready to use INDI-profile created with INDI Web Manager.
5. Raspberry PI (or computer) where you run `indihub-agent` is connected to Internet so it can register your equipment on INDIHUB-network.

## Registration on INDIHUB-network as a host

Registration is very easy - you don't have to do anything.

The `indihub-agent` doesn't require any signup or token to join INDIHUB.

When you first run `indihub-agent` and it is connected to INDIHUB-network successfully - it receives token from network and saves in the same folder in the file `indihub.json`. Please keep this file there and don't loose it as it identifies you as a host on INDIHUB-network. Also, this file will be read and used automatically for all next runs of `indihub-agent`. 

## indihub-agent modes

There are three modes available at the moment:

1. `share` - open remote access to your equipment via INDIHUB-network of telescopes, so you can provide remote imaging sessions to your guests.
2. `solo` - use you equipment without opening remote access but equipment is still connected to INDIHUB-network and all images taken are contributed for scientific purposes. 
3. `robotic` - open remote access to your equipment to be controlled by scheduler running in INDIHUB-cloud (experimental).

The mode is specified via `-mode` parameter, i.e. to run indihub-agent in a share-mode you will need run command:

```bash
./indihub-agent -indi-profile=my-profile -mode=share
```

The only mandatory parameter is `-indi-profile` where you specify profile name created with [INDI Web Manager](https://github.com/knro/indiwebmanager). All other parameters have default valyes. I.e. `-mode` default value is `solo`.

To get usage of all parameters just run:
```bash
./indihub-agent -help
```

The latest `indihub-agent` release can be downloaded from [releases](https://github.com/indihub-space/agent/releases) or [indihub.space](https://indihub.space) Web-site. 

## API

There is an API-server running as part of `indihub-agent` and listening on port `:2020` (or on port specified via `-api-port=N` parameter) which provides two different APIs to control or use `indihub-agent`:

- RESTful API to get status or switch modes of the agent
- Websocket API to control equipment via websocket-connections

By default API-server works over HTTP-protocol. You can switch it to work over TLS by providing `-api-tls` parameter. This will make `indihub-agent` to generate self-signed CA and certificate.

### RESTful API

You can use this simple RESTful API to control `indihub-agent`.

NOTE: CORS protection can be specified with comma-separated list of allowed origins via agent parameter:

`-api-origins=host1,host2,hostN`.

Also, all examples assume `indihub-agent` is running on host `raspberrypi.local`.

#### 1. Get indihub-agent status (public)

`curl -X GET http://raspberrypi.local:2020/status`

Response example:
```json
{
    "indiProfile": "NEO-remote",
    "indiServer": "raspberrypi.local:7624",
    "mode": "solo",
    "phd2Server": "",
    "status": "running",
    "supportedModes": [
        "solo",
        "share",
        "robotic"
    ],
    "version": "1.0.3"
}
```

#### 2. Restart indihub-agent current mode (public)

`curl -X GET "http://raspberrypi.local:2020/restart"` 

#### 3. Switch indihub-agent mode (protected via token)

You need to do HTTP-request `POST "http://indihub-agent-host:2020/mode/{mode}"` specifying required mode in a `mode` URI-parameter and supplying your token in `Authorization` header, i.e.:

```bash
curl -X POST "http://raspberrypi.local:2020/mode/share" -H "Authorization: Bearer cca13ac2951efd6d912ead20a7ab4882"
```

Response will have status of agent running in new mode:

```json
{
    "indiProfile": "NEO-remote",
    "indiServer": "raspberrypi.local:7624",
    "mode": "share",
    "phd2Server": "",
    "publicEndpoints": [
        {
            "name": "INDI-Server",
            "addr": "node-1.indihub.io:55642"
        }
    ],
    "status": "running",
    "supportedModes": [
        "share",
        "robotic",
        "solo"
    ],
    "version": "1.0.3"
}
```

### Websocket API

You can use `indihub-agent` to control your equipment via Websocket API, i.e. from your Web-app open int the Web-browser.

NOTE: Websocket upgrade-requests are allowed only from origins specified with comma-separated list via agent parameter:

`-api-origins=host1,host2,hostN` 

#### 1. Open WS-connection to INDI-server (protected via token)

To establish a new WS-connection to INDI-server you will need to use URL with format: 

`ws://raspberrypi.local:2020/websocket/indiserver?token=cca13ac2951efd6d912ead20a7ab4882`

NOTE: we connect to `raspberrypi.local:2020` which is `indihub-agent` API-server and we provide token via `token` query string parameter.

This will open WS-connection to you your equipment via `indihub-agent` API-server where all outgoing messages will be INDI-protocol commands and all incoming messages will be INDI-protocol replies from your equipment. 

#### 2. Message format for INDI-server Websocket connection

All commands are expected to be in JSON-format where INDI-command XML attributes translate to fields with `attr_`-prefix.

I.e. to send this INDI XML-command over WS-connection:

```xml
<getProperties version="1.7" />
```

You will need to send message over WS-connection in JSON-format:
```json
{
  "getProperties": {
    "attr_version":"1.7"
  }
}
```

The INDI-protocol responses will be converted from XML to JSON-messages and sent over WS-connection as messages.

I.e. this INDI-response about telescopes installed on the mount in XML-format:
```xml
<defNumberVector device="iOptron CEM25" name="TELESCOPE_INFO" label="Scope Properties" group="Options" state="Ok" perm="rw" timeout="60" timestamp="2020-02-20T21:52:23">
    <defNumber name="TELESCOPE_APERTURE" label="Aperture (mm)" format="%g" min="10" max="5000" step="0">
120
    </defNumber>
    <defNumber name="TELESCOPE_FOCAL_LENGTH" label="Focal Length (mm)" format="%g" min="10" max="10000" step="0">
600
    </defNumber>
    <defNumber name="GUIDER_APERTURE" label="Guider Aperture (mm)" format="%g" min="10" max="5000" step="0">
50
    </defNumber>
    <defNumber name="GUIDER_FOCAL_LENGTH" label="Guider Focal Length (mm)" format="%g" min="10" max="10000" step="0">
162
    </defNumber>
</defNumberVector>
```

Will be converted in to JSON-format like this:

```json
{
  "defNumberVector": {
      "attr_device":"iOptron CEM25",
      "attr_group":"Options",
      "attr_label":"Scope Properties",
      "attr_name":"TELESCOPE_INFO",
      "attr_perm":"rw",
      "attr_state":"Ok",
      "attr_timeout":60,
      "attr_timestamp":"2020-02-20T21:52:23",
      "defNumber": [
        {
          "#text":120,
          "attr_format":"%g",
          "attr_label":"Aperture (mm)",
          "attr_max":5000,
          "attr_min":10,
          "attr_name":"TELESCOPE_APERTURE",
          "attr_step":0
        },
        {
          "#text":600,
          "attr_format":"%g",
          "attr_label":"Focal Length (mm)",
          "attr_max":10000,
          "attr_min":10,
          "attr_name":"TELESCOPE_FOCAL_LENGTH",
          "attr_step":0
        },
        {
          "#text":50,
          "attr_format":"%g",
          "attr_label":"Guider Aperture (mm)",
          "attr_max":5000,
          "attr_min":10,
          "attr_name":"GUIDER_APERTURE",
          "attr_step":0
        },
        {
          "#text":162,
          "attr_format":"%g",
          "attr_label":"Guider Focal Length (mm)",
          "attr_max":10000,
          "attr_min":10,
          "attr_name":"GUIDER_FOCAL_LENGTH",
          "attr_step":0
        }
      ]
  }
}
```

Please NOTE:

- XML element attributes get converted into JSON-fields with `attr_` prefix
- XML element value get converted into JSON-field with name `#text`
- vector like child-elements get converted into JSON-arrays

## Building indihub-agent

You will need to install [Golang](https://golang.org/dl/).

There are `make` build-commands for different platforms available:

- `make build-macos64` - build for macOS (64 bit)
- `make build-linux64` - build for Linux (64 bit)
- `make build-unix64` - build for Unix (64 bit)
- `make build-win64` - build for Windows (64 bit)
- `make build-win32` - build for Windows (32 bit)
- `make build-raspberrypi` - build for Raspberry Pi (ARM5)

## Contributing

PRs and issues are highly appreciated. TODO: `indihub-agent` speaks to the INDIHUB-cloud so special dev-server will be added for development purposes soon.

## What is next

The INDIHUB-network is in its beta release at the moment. We board new hosts on the network, collect data and most importantly - feedback from our first hosts.

Also we work on partnerships. If you are interested please send us email at [info@indihub.space](mailto:info@indihub.space).

And last but not least - don't forget to signup to our mailing list on [indihub.space](https://indihub.space) so you will know all the news first!