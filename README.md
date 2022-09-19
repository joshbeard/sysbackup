# sysbackup

A simple shell script that uses `rsync` to perform backups.

This was a quick hack to get a job done and there's probably a "real" tool out there that other people would prefer or their own scripts
around _rsync_.

I've tested this on Linux (Ubuntu/Debian), FreeBSD, and macOS.

This script was created in 2011 and has been dormant since. It still works in $CURRENT_YEAR, though.

## Usage

1. Copy the [`example.conf`](example.conf) to a new file and adjust the settings.
2. Copy the [`filter.txt`](filter.txt) to a new file and adjust as needed.
3. Run `sysbackup.sh --config <path/to/config>` interactively or via cron.

SSH authentication is used. Ensure the private key is unlocked when the script runs.

## Use Case

Create incremental backups that match the source data's directory hierarchy:

Source data:

```plain
	homes/
		- dept1/
			- staff
			- others
		- dept2
		- dept3
```

Backup Data:

```plain
	/data/backups/homes/
		- dept1
			- 2012-01-31
			- 2012-01-30
			- 2012-01-29
			- Latest -> 2012-01-31
		-dept2
			....
```

This allows sharing the backup data using standard sharing protocols (AFP, SMB, NFS)
and for users to easily browse the backups to drag/drop their data.

This uses [hard links](https://en.wikipedia.org/wiki/Hard_link) to reduce data duplication.

## Notes

Notes for ZFS NFSv4 ACLs
	- These don't seem to transfer. rsync?

Notes for Mac OS X:
	- The version of rsync bundled with <10.6 is pretty old and missing a lot of options.
	- Look for a more recent version (even as a client)

