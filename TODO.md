# TODO

* Update for best practices
* Trackdown an issue where backups aren't created and 'Latest' is symlinked against an empty file.
  * This seems to occur if the remote data is unavailable or we don't have permission to backup
* Use logger(1) for logging
* Port to Bourne shell (sh)
* More error/sanity checks
* Obtain real/true size of backup before running (how big it will be with hard links considered)
* Argument parsing
