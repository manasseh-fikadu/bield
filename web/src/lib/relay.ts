// How the UI suggests a peer relay: the command that makes a machine one,
// and the grant in the tailnet policy file that lets devices use it.

/** The UDP port a peer relay listens on unless the admin picks another, as in Tailscale's documentation. */
export const defaultRelayPort = 40000;

/** Whether n is a UDP port a relay can listen on. */
export function validPort(n: number): boolean {
	return Number.isInteger(n) && n >= 1 && n <= 65535;
}

/**
 * The command that makes a machine running os a peer relay on port. It needs
 * root on Linux and the BSDs.
 */
export function relayCommand(os: string, port: number): string {
	const command = `tailscale set --relay-server-port=${port}`;
	return ['macos', 'windows'].includes(os.toLowerCase()) ? command : `sudo ${command}`;
}

/** The grant that lets the devices at the addresses in src use the relay at dst. */
export function relayGrant(src: string[], dst: string): string {
	const list = src.map((ip) => JSON.stringify(ip)).join(', ');
	return `{\n  "src": [${list}],\n  "dst": [${JSON.stringify(dst)}],\n  "app": { "tailscale.com/cap/relay": [] }\n}`;
}
