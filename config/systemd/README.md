# Sysbackup Systemd Timer

1. Create a service file in `/etc/systemd/system/sysbackup.service` for
   system-wide config or `~/.config/systemd/user/sysbackup.service` for user
   config.

   See [`sysbackup.service`](sysbackup.service)

2. Create a timer file in `/etc/systemd/system/sysbackup.timer` for
   system-wide config or `~/.config/systemd/user/sysbackup.timer` for user
   config.

   See [`sysbackup.timer`](sysbackup.timer)

3. Enable and start the timer

    ```shell
    sudo systemctl enable --now sysbackup.timer
    sudo systemctl start sysbackup.timer
    ```

    or for user config:

    ```shell
    systemctl --user enable --now sysbackup.timer
    systemctl --user start sysbackup.timer
    ```

