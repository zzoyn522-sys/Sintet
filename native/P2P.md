# Sintet friends and P2P

The launcher and game agent share `socialManager` through an authenticated HTTP
listener bound exclusively to `127.0.0.1`. Each launcher process generates a new
256-bit IPC secret. The agent gets only that local secret, the port, and display
data. The Sintet ID session stays inside Go. Browser-origin requests are rejected.

The independent `sintet-identity` Cloudflare Worker authenticates the existing
encrypted desktop session. Private Supabase tables hold friendships, expiring
presence, rooms, invitations and offer/answer descriptions. RLS denies public and
anonymous access. The portal does not need to be running. No game bytes pass
through Cloudflare or Supabase.

Pion WebRTC v4 transports ordered, reliable SCTP messages over DTLS. A message-to-
stream adapter preserves message boundaries while exposing an `io.ReadWriteCloser`
to yamux. Each accepted Minecraft TCP connection gets its own multiplexed stream.
The host can dial only `127.0.0.1` at the port detected in the current game's LAN
startup log. The guest TCP listener accepts only connections originating on that
same computer. It broadcasts the actual listening port through Java Edition LAN
multicast. A direct `127.0.0.1:port` address is also displayed in both interfaces.

Opening the launcher settings' auto-hide option keeps Go alive while the overlay
and P2P need it; the window is shown again after the game exits. Explicitly killing
the launcher process terminates its network connections.

## Operation

Both users sign in through Sintet ID, add each other and accept the friend request.
The host can create a lobby in the launcher before starting Minecraft, selecting
an installed instance. Guests join by invitation or six-digit code and prepare
the shared instance while waiting for the host to open a LAN world.

Room membership, the encrypted file channel, and the Minecraft endpoint have
separate lifetimes. A dormant host peer uses `NewPeer(-1, ...)`; `SetHostPort`
enables future game streams after the selected instance exposes its LAN socket.
Closing the world clears that endpoint without closing the lobby or file channel.
The host's selected profile must match the running game before it is advertised.

The authenticated P2P `/lobby` exchange carries world readiness and bounded
participant preparation states. Download consent is tied to a manifest digest;
each peer keeps the advertised snapshot for ongoing downloads. The manifest is
refreshed before host launch, before exposing a newly opened world, or explicitly
by the leader. Guests recheck a changed digest without overwriting their files.

Preparation alone never launches or closes Minecraft. Guests explicitly choose
to launch/join/restart once the world is ready. An opt-in auto-connect checkbox
performs the same action through the serialized launcher action queue. The
launcher stays alive in the tray for lobby launches even with the quit-on-launch
preference. Both computers need the updated launcher for pre-game lobbies.

Local automated coverage includes dormant-host file transfer, opening and changing
LAN endpoints without recreating WebRTC, prepared guests waiting without launch,
progress authorization, and headless launcher UI states. Actual Minecraft gameplay
on two computers has not been retested for this change.

## Verified

- Hosted worker: search, request/accept/remove, presence, invitation, code, SDP
  ownership checks, room closure and cascading cleanup, with disposable accounts.
- Two real Go clients with hosted signaling and STUN: 1 MiB TCP echo through P2P,
  then a fresh reconnect to the same room and a second intact 1 MiB transfer.
- Closed sessions cannot overwrite a newer connection endpoint, ping or status.
  Reconnects get new signaling IDs instead of reusing stale SDP answers.
- Local Pion peers: three simultaneous 2 MiB streams and listener cleanup.
- Real Windows multicast announcement: the advertised address reaches the tunnel.
- Native C++ IPC client: state, action and search, with no browser or game needed.
- Local IPC rejects missing/wrong secrets and browser Origin headers.

Run `native\build.bat test`, then `_tools\go\bin\go.exe test . ./internal/social
./internal/identity ./internal/launcher`. The portal script
`scripts/test-social-live.mjs` requires `SINTET_RUN_LIVE_SOCIAL=1`, creates its own
temporary accounts, runs the hosted + Go checks and removes the fixtures.

## Remaining verification and limits

- Actual Minecraft join on two separate computers/networks has not been run.
  The user confirmed on 2026-11-09 that a second computer is not available and
  previously requested to do Minecraft gameplay tests themselves. Do not launch
  their Minecraft instance automatically or claim that this gate has passed.
- The renderer now hooks Windows OpenGL SwapBuffers, covering the presentation
  path used by both LWJGL 2 and GLFW/LWJGL 3. Old/new game versions, fullscreen,
  cursor handling and visual transitions still need actual in-game checks.
- Current build is Windows x64 and OpenGL; 32-bit JVMs and renderer-replacement
  Vulkan mods are not supported by this native DLL.
- STUN-only connectivity is not guaranteed through symmetric NAT or restrictive
  firewalls. No TURN relay is configured, and no universal 1–2 second connection
  time or 30–50 ms latency is promised. Errors are surfaced in both interfaces.
- DTLS encryption is provided by Pion, not a guarantee of hardware acceleration.
- Friends currently use Sintet ID identities. Local/Ely.by/Microsoft-only profiles
  must sign into Sintet ID to access that social graph.

References: [Pion](https://github.com/pion/webrtc),
[Windows multicast loopback semantics](https://learn.microsoft.com/en-us/windows/win32/winsock/ip-multicast-2).
