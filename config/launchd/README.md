# Sysbackup Launchd Agent

1. Create a plist file in `/Library/LaunchDaemons/dev.jbeard.sysbackup.plist`
   for system-wide config or `~/Library/LaunchAgents/dev.jbeard.sysbackup.plist`
   for user config.

   See [`dev.jbeard.sysbackup.plist`](dev.jbeard.sysbackup.plist)

2. Load the LaunchDaemon

    ```shell
    sudo launchctl load /Library/LaunchDaemons/dev.jbeard.sysbackup.plist
    ```

    or for user config:

    ```shell
    launchctl load ~/Library/LaunchAgents/dev.jbeard.sysbackup.plist
    ```
