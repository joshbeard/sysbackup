# sysbackup

<img align="right" width="120" height="146" src=".github/disk.png">

A simple rsync wrapper for creating incremental backups.

This replaces a couple of rsync wrapper shell scripts I've used over the years.

## Install

```shell
curl -sfL https://raw.githubusercontent.com/joshbeard/sysbackup/master/install.sh | sh

```

<details>
<summary>Installation Details and Customization</summary>

The latest release can be found on the [releases](https://github.com/joshbeard/sysbackup/releases)
page and can be downloaded and installed manually.

To download and install the latest version of sysbackup using `curl` and piping it
to the shell, run the following command:

```sh
curl -sfL https://raw.githubusercontent.com/joshbeard/sysbackup/master/install.sh | sh -
```

<details>

<summary>What the installation script does</summary>

- Detects your OS and architecture.
- Downloads the latest release of sysbackup from GitHub.
- Verifies the checksum of the downloaded package.
- Extracts the binary and moves it to the specified directory (default is `$HOME/bin`).

</details>

Make sure the installation directory is in your `PATH` so you can easily run
`sysbackup` from anywhere.

### Custom Installation Directory

If you want to specify a custom installation directory, you can set the
`INSTALL_DIR` environment variable or pass the `-d` (or `--dir`) argument. For
example:

```sh
# Using INSTALL_DIR environment variable
INSTALL_DIR=/usr/local/bin curl -sfL https://raw.githubusercontent.com/joshbeard/sysbackup/master/install.sh | sh -

# Using -d (or --dir) argument
curl -sfL https://raw.githubusercontent.com/joshbeard/sysbackup/master/install.sh | sh -s -- -d /usr/local/bin
```

### Install from Source

To install from source, clone the repository and run `go build`:

```sh
git clone https://github.com/joshbeard/sysbackup.git
cd sysbackup
go build -o sysbackup .
mv sysbackup ~/bin
```

</details>

## Usage

```shell
sysbackup /path/to/source /path/to/destination
```

This will create `/path/to/destination/YYYY-MM-DD/source/` using rsync.
Subsequent backups use hardlinks and link back to the previous backup. A
`Latest` symlink is created to the most recent backup.

The program behavior can be customized using CLI flags, environment variables,
or configuration files.

### Backup Directory Structure

A directory will be created within the target in a configurable date format
(`YYYY-MM-DD-HH-MM` by default). The source directory is copied to this
date-stamped directory. Subsequent backups will use hardlinks to the previous
backup to save space. A `Latest` symlink is created to the most recent backup.

Be aware of how rsync handles trailing slashes on the source directory:

* If the source directory has a trailing slash, the contents of the source
  directory will be copied to the target directory. (e.g. `target/YYYY-MM-DD/`)
* If the source directory does not have a trailing slash, the source directory
  itself will be copied to the target directory. (e.g. `target/YYYY-MM-DD/source/`)

**Example source directory:**

```plain
source
├── a-directory
│   └── foo.txt
└── example.txt
```

**Example target directory:**

With a trailing slash on the source directory:

```plain
target
├── 2024-09-29-11-40
│   ├── a-directory
│   │   └── foo.txt
│   └── example.txt
└── Latest -> 2024-09-29-11-40
```

Without a trailing slash on the source directory:

```plain
target
├── 2024-09-29-11-39
│   └── src
│       ├── a-directory
│       │   └── foo.txt
│       └── example.txt
└── Latest -> 2024-09-29-11-39
```

The same target directory shouldn't be used for other purposes.

## Configuration

See [`config/`](config/) for example configurations.
