# indihub-agent

The `indihub-agent` is a command line interface tool to connect your astro-photography equipment to [INDIHUB](https://indihub.space) network. The network can be used to share you equipment with others, use others' equipment remotely or just use your equipment without sharing but still contribute your images to INDIHUB-network.

NOTE: all astro-photos taken via INDIHUB-network (with auto-guiding or main imaging cameras) will be processed by INDIHUB cloud pipeline and used for scientific purposes.

## Prerequisites

0. You have a desire to contribute to Space exploration and sustainability projects.
1. You have motorized astro-photography equipment connected to your home network.
2. Your equipment is controlled by [IND-server](https://github.com/indilib/indi), manuals and docs can be found on [INDI-lib](http://indilib.org) Web-site.
3. INDI-server is controlled by [INDI Web Manager](https://github.com/knro/indiwebmanager).
4. You have ready to use INDI-profile created with INDI Web Manager.
5. Raspberry PI (or computer) where you run `indihub-agent` is connected to Internet so it can register your equipment on INDIHUB-network.

## Registration on INDIHUB-network as a host

Registration is very easy - you don't have to do anything.

The `indihub-agent` doesn't require any signup or token to join INDIHUB.

When you first run `indihub-agent` and it is connected to INDIHUB-network successfully - it receives token from network and saves in the same folder in the file `indihub.json`. Please keep this file there and don't loose it as it identifies you as a host on INDIHUB-network. Also, this file will be read and used automatically for all next runs of `indihub-agent`. 

## indihub-agent modes

There are four modes available at the moment:

1. `share` - open remote access to your equipment via INDIHUB-network of telescopes, so you can provide remote imaging sessions to your guests.
2. `solo` - use you equipment without opening remote access but equipment is still connected to INDIHUB-network and all images taken are contributed for scientific purposes. 
3. `broadcast` - broadcast you imaging session to observers watching it via INDI-clients, in this case without any equipment remote access and sharing (experimental).
4. `robotic` - open remote access to your equipment to be controlled by scheduler running in INDIHUB-cloud (experimental).

The mode is specified via `-mode` parameter, i.e. to run indihub-agent in a share-mode you will need run command:

```bash
./indihub-agent -indi-profile=my-profile -mode=solo
```

The only mandatory parameter is `-indi-profile` where you specify profile name created with [INDI Web Manager](https://github.com/knro/indiwebmanager). All other parameters have default valyes. I.e. `-mode` default value is `solo`.

To get usage of all parameters just run `indihub-agent -help`.

The latest `indihub-agent` release can be downloaded from [releases](https://github.com/indihub-space/agent/releases) or [indihub.space](https://indihub.space) Web-site. 

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

The INDIHUB-network is in its beta release at the moment. We board new host on the network, collect data to most importantly - feedback from our first hosts.

Also we work on partnerships. If you are interested please send us email at [info@indihub.space](mailto:info@indihub.space).

And last but not least - don't forget to signup to our mailing list on [indihub.space](https://indihub.space) so you will know all the news first!