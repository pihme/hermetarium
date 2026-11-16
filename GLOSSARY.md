# Hermetarium

The words for one sealed place a software agent lives in, and for the parts around it.

## Language

**Hermetarium**:
The product: the supervisor and the system around a habitat.
_Avoid_: habitat

**Habitat**:
One running place: an image, a wall, and the network path.
_Avoid_: box, Hermetarium

**Inhabitant**:
The process that lives inside the image.
_Avoid_: assistant

**Image**:
The OCI image. It is the Linux the inhabitant sees.
_Avoid_: root disk

**Wall**:
The isolation around the image. A weak wall shares the host kernel. A strong wall is a Firecracker microVM with its own kernel.
_Avoid_: sandbox

**Supervisor**:
The control plane that starts and stops a habitat, owns the network path, and writes the I/O log.
_Avoid_: orchestrator

**Gate**:
Squid, the only network path between the habitat and the outside. On the strong wall it is not the Firecracker container.
_Avoid_: helper

**I/O log**:
The record of traffic that crosses the wall.
_Avoid_: access log

## Strong wall

**Firecracker container**:
The container on the strong wall that holds the microVM.
_Avoid_: helper

**Firecracker scripts**:
The strong-wall scripts in `firecracker-helper/`. They are not the Firecracker container.
_Avoid_: helper, Firecracker helper

## Host programs

**Build image**:
The small Alpine image the supervisor uses for its own jobs, outside any habitat.
_Avoid_: helper

**Porter**:
The host program that carries the operator's question to an inhabitant.
_Avoid_: helper
