# FAQ

## About Bield

### How is Bield different from Beszel, Uptime Kuma, Portainer, Cockpit and the Tailscale admin console?

Each of these does its own job well, and some do it better than Bield. Bield's job is narrower: one page for a small tailnet, with the tailnet as the login. It's new, and they are older and better tested. If one of them already covers what you need, keep it. They run fine next to Bield.

**Beszel** is the closest. It has a hub and an agent on each machine, with CPU, memory, disk, network and container stats, history and alerts, under the MIT license. It collects more than Bield's agent does (GPU, S.M.A.R.T. disk health, ZFS), has its own users with OAuth and OIDC sign-in, and can back up its data to S3. Bield adds what comes from being on the tailnet: every device in the Tailscale device list, with its route and key expiry and no agent installed, and then terminals, files and container actions. If you only want metrics, Beszel is the lighter and more mature choice.

**Uptime Kuma** watches services and tells you when they stop answering. It has more kinds of monitor than Bield's website, port and DNS checks, public status pages, and over 90 notification services. Bield's checks run from the machine Bield is on and feed its alerts and health score, and it pings every device. Use Uptime Kuma if you want a status page, or to watch from outside your network. Bield sends to ntfy, Telegram, email, Discord, Slack and webhooks ([Notifications](notifications.md)).

**Portainer** is a full container manager. It creates and deploys containers and stacks, and its Business Edition adds roles, SSO and audit logs for teams. Bield can list containers, read their logs and stats, and start, stop and restart them. It can't create, exec or prune, and that's on purpose. What it adds is the container list next to the rest of what's wrong with the machine. To deploy with Docker, use Portainer or Compose files.

**Cockpit** is a web admin session on one machine: a terminal, services, storage, networking and logs. It reaches other machines too, if each runs Cockpit and you sign in with that machine's own accounts. It goes much deeper into one machine than Bield does, which has no storage or network settings. Bield goes wider. It shows the whole tailnet at once, phones and laptops included, and it needs nothing on a machine except sshd and, for metrics, a small bash agent.

**The Tailscale admin console** is where you manage the tailnet itself: devices, keys, access controls and DNS. Bield doesn't replace it and doesn't change your tailnet's settings. It reads the device list and puts it beside what each machine is doing, which the console doesn't show: CPU, memory, disk, containers and alerts. You still use the console to give Bield the access it needs ([Bield and Tailscale](tailscale.md)).

### Is v0.1 ready for production?

Not yet, for anything you can't afford to get wrong. v0.1 is the first release line. It has had an outside security review, and v0.1.1 fixes what that found, but it's young. It can open a shell on every machine you give it SSH access to, so a bug in Bield can be a bug on all of them. [SECURITY.md](../SECURITY.md) has the threat model.

The [release notes](https://github.com/manasseh-fikadu/bield/releases) say what was run on real machines and what wasn't. The API can change in any release ([Is there an API?](#is-there-an-api)). For a homelab or a few machines you can recover, it's reasonable to try it today. Run it next to your existing monitoring, not instead of it, and take a backup before each upgrade ([Upgrading, backups and rolling back](upgrading.md)). For a tailnet that holds production servers, wait until more people have tried to break it, or read the code you're trusting first.

### What's the license, and can I use it at work?

Bield is under the GNU Affero General Public License v3.0 (AGPL-3.0). The agent in `agent/` is under the MIT License.

Running Bield as it is, at work or at home, asks nothing of you. You pay nothing and publish nothing. The AGPL has one extra rule, and it's about changes: if you modify Bield and other people use your version over a network, colleagues included, you have to offer them the source of your version under the same license. Running Bield next to your own software doesn't make that software AGPL.

Some companies ban AGPL software outright. Ask yours before you put Bield on a work tailnet. This isn't legal advice. The agent is MIT so that you can copy and change the script that runs on your machines without asking that question.

### What does "Bield" mean?

Bield is a Scots word for shelter or refuge, said "beeld". Bield Housing & Care (bield.co.uk), a Scottish housing association for older people, uses the same word for its name. This project has nothing to do with it, and it isn't affiliated with Tailscale Inc. or the Headscale project either.

## Setting up

### Do I need a Tailscale account?

You need a tailnet, which is Tailscale's, or a [Headscale](headscale.md) server that you run. Bield has no passwords. It asks the tailnet who is connecting. To look around first, `bield serve --demo` needs no account ([Demo mode](demo.md)).

### Do I have to open a port or set up a reverse proxy?

No. Bield listens only on the tailnet. In embedded mode that's port 443 of its own node (with port 80 redirecting to it), or port 80 alone when the tailnet has no HTTPS certificates, and in host mode it's port 4700 on the machine's tailnet addresses. The login page that embedded mode shows at first is on `127.0.0.1:4700`, which is the machine itself. Bield trusts the tailnet to say who is connecting, so don't publish it on the open internet ([how to check](#how-do-i-keep-bield-off-the-public-internet)).

### How do I keep Bield off the public internet?

You don't have to do anything. Bield serves the app only on the tailnet, as the answer above says. Putting it on the open internet takes something you add:

- A reverse proxy or a tunnel in front of it. Don't. Bield works out who is calling from the tailnet connection. Through a proxy it can't tell, so every caller is anonymous, and an anonymous caller still gets the app's files and the health check.
- Docker's `ports:` for 4700 on all interfaces. Until Bield is logged in to Tailscale, anyone who opens that page can add Bield to their own tailnet. Publish it on the host's loopback only, `127.0.0.1:4700:4700`, or leave it unpublished ([Bield in Docker](docker.md)).
- `BIELD_SETUP_LISTEN` set to more than loopback. Bield warns about it in its log.
- `--demo-listen 0.0.0.0:4700` on a machine that anyone can reach. The demo takes everyone for an admin ([Demo mode](demo.md)).

To see what Bield listens on, run `sudo ss -tlnp | grep bield`. In embedded mode you should see only `127.0.0.1:4700`. In host mode you should see the machine's tailnet addresses on port 4700, which start with `100.`. With Docker, `docker compose ps` should show no published ports.

### Embedded or host mode?

Use embedded if you aren't sure. It works anywhere, including in a container, and Bield shows up as its own machine. Use host mode when the machine already runs Tailscale and you want Bield to be that machine. [Bield and Tailscale](tailscale.md) has the table.

### Bield asks me to log in to Tailscale every time it starts.

It forgot its node, which lives in `tailscale/` in the data directory. In Docker that means the container has no volume for `/data`, or a different one each time. Use the volume in `compose.yaml`. With the install script, check that `/var/lib/bield` survives.

### The page says it's not secure, or the browser warns about HTTP.

Bield serves HTTPS when the tailnet has HTTPS certificates turned on. Turn on MagicDNS and HTTPS in the [DNS settings](https://login.tailscale.com/admin/dns). Without them, Bield serves plain HTTP, which WireGuard still encrypts. Headscale makes no certificates, so it's always HTTP there. See [HTTPS](tailscale.md#https).

### Which systems does it run on?

The release archives are for Linux and macOS, on amd64 and arm64, and the Docker image is for Linux. Bield measures the machine it runs on, and watches its systemd, only on Linux. Docker works wherever Bield can reach the socket. On a Mac it still manages the tailnet, and the [agent](agent.md) reports the Mac's metrics. There's no Windows build, and no agent for Windows, iOS or Android, though those devices show up in the device list.

## Signing in and roles

### I'm signed in, but Bield says I can't view it.

Bield knows you, and you have no role. By default, only admins and viewers can see anything. An admin runs `bield admin add-viewer you@example.com`, or sets who can view to the whole tailnet. [Admins and viewers](tailscale.md#admins-and-viewers) has the details. The page shows the command to run.

### Bield thinks I'm anonymous.

A request is anonymous when Tailscale can't say which user it comes from: a tagged device, an address Tailscale doesn't know, or Bield's own machine in host mode. In host mode, open Bield from another device. A script or an agent on such a machine can use an [API token](cli.md#api-tokens) instead. `GET /api/me` says who Bield thinks you are, and why not.

### I'm locked out, and no admin can sign in.

On the machine Bield runs on, `bield admin add you@example.com` adds an admin straight in the database, even while Bield is running. For the install script, run it as the `bield` user with `BIELD_DATA_DIR` set ([Installing Bield](install.md#running-it)). In Docker: `docker compose exec bield bield admin add you@example.com`.

### Who is the first admin?

Whoever logs Bield in to Tailscale, and anyone in `BIELD_ADMINS`. If Bield's node is tagged, it has no owner, so set `BIELD_ADMINS`.

## Machines and metrics

### A machine shows no CPU or memory.

Tailscale doesn't report those. Install [the agent](agent.md) on it. Bield measures the machine it runs on by itself, if it's Linux.

### A laptop or phone keeps raising "offline" alerts.

Turn off **Alert when offline** in the device's settings (it's off for phones and tablets already), or raise the offline threshold for it or for everything, in [Settings](settings.md#alerts). A device is only alerted about once Bield has seen it online.

### The terminal or files page says the machine has no SSH access.

Bield has to log in to the machine first. Open the machine's page and set up the **SSH access** card ([SSH access](ssh.md)).

### The terminal says the host key changed.

Bield pinned the machine's SSH host key the first time and refuses a different one. If you reinstalled the machine, press **Trust the new key** on its SSH access card. If you didn't, find out why before you do. See [Host keys](ssh.md#host-keys).

### Docker says Bield can't reach it.

The user Bield runs as can't use the Docker socket. With the install script, `sudo usermod -aG docker bield` and restart ([Installing Bield](install.md#docker-on-the-same-machine)). In a container, mount the socket and add its group ([Bield in Docker](docker.md#managing-the-machines-containers)). For another machine, the SSH user must be able to use Docker. The `docker` group is as good as root, so decide that on purpose.

### Why can't a viewer see a container's logs?

Viewers get a smaller view of containers, on purpose: no logs, commands, environment variables or inspection, since those can hold secrets. Admins get all of it. See [What viewers see](pages.md#what-viewers-see).

## Data and privacy

### Where does Bield keep its data?

In the data directory: a SQLite database, the embedded node's identity, and Bield's SSH key. [The configuration reference](configuration.md#the-data-directory) lists it, and [Upgrading, backups and rolling back](upgrading.md) says how to back it up and restore it, including onto another machine.

### Does Bield send my data anywhere?

Bield has no analytics and doesn't check for updates. What leaves your machine:

- The embedded node talks to Tailscale (or your Headscale server), like any node. It also uploads its logs to Tailscale unless you set `TS_NO_LOGS_NO_SUPPORT=true`.
- The three default checks in the group Internet reach `1.1.1.1`, `cloudflare.com` and `google.com`. Delete them on the Checks page to stop that.
- Notification channels send to the services you set up.

### Is it safe to store notification tokens in Bield?

They're in the database as text, and the database file is readable only by Bield's user. Backups hold them too. Bield never shows a saved secret again. See [Notifications](notifications.md).

### How much does it cost to run?

With 12 devices it uses about 0.4% of a core and 59 MB of memory. With 200 devices and 90 days of history, about 3% of a core and 129 MB, and a database of 580 MB. The numbers are from a small VPS. See [the benchmark](benchmark.md).

## Alerts and notifications

### How do I get alerts on my phone?

Add an ntfy channel in Settings, Notifications, and subscribe to its topic in the ntfy app. Telegram, email, Discord, Slack and a generic webhook work too ([Notifications](notifications.md)).

### An alert opened and resolved by itself. Is something wrong?

An alert resolves a minute after its condition goes away, and a condition that comes and goes stays one alert. If one keeps flapping, raise the threshold or the number of failures in a row, or lengthen the CPU window.

### I acknowledged an alert, and it came back.

Acknowledging an alert is for that alert. If the problem goes away and returns after the alert resolved, it's a new alert. A snooze ends at its time, and the alert then counts as open again if the problem is still there.

## Upgrading and developing

### How do I upgrade, and can I go back?

Run the install command again, or pull the new image. Bield saves a backup first and migrates the database when it starts. Going back needs the older binary and the backup from before the upgrade. See [Upgrading, backups and rolling back](upgrading.md).

### Bield says "the database was written by a newer version".

You started an older Bield on a database that a newer one has already changed. Bield refuses, so it can't damage it. Install the newer version again, or [roll back](upgrading.md#rolling-back) with a backup from before the upgrade.

### Is there an API?

The web app uses an HTTP API under `/api`. It isn't documented or versioned yet, so it can change in any release, and it's better not to build on it. A request that changes something needs an `X-Bield` header ([Programs that call the API](tailscale.md#programs-that-call-the-api)).

### How do I report a bug or a security problem?

Bugs go in a [GitHub issue](https://github.com/manasseh-fikadu/bield/issues). Security problems go through GitHub's private reporting, as [SECURITY.md](../SECURITY.md) says.
