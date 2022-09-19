#!/bin/bash
#############################################################################
# sysbackup
#
# Fairly simple backup script, using rsync
#
# Refer to the 'example.conf' provided for an example config file and pass
# it to 'sysbackup.sh' using the '-c' or '--config' argument.
#############################################################################

# Set the trap early
trap my_trap INT

case "$1" in
  -c|--config)
    config_file=$2
  ;;
esac

function usage() {
  echo "Usage: $0 -c|--config CONFIG_FILE"
  echo
  echo "Arguments:"
  echo "  -c, --config    Path to a config file."
  echo "                  This can also be set using the SYSBACKUP_CONFIG environment variable."
  echo
}

# Load config file
if [ ! -z "$config_file" ]; then
  if [ -e "$config_file" ]; then
    if ! source "$config_file"; then
      err="FAILURE: Unable to include $config_file\n"
      log_print "$err" ; send_email "$err"
      quit 1
    fi
  else
    echo "FAILURE: config_file is set, but $config_file doesn't exist."
    exit 1
  fi
else
  echo "FAILURE: a config file must be set."
  usage
  exit 1
fi


#############################################################################
# Functions
#############################################################################

function log() {
  if [ ! -z "${log_file}" ]; then
    printf "$(date "${log_date_fmt}")$1" >> "${log_file}"
  fi
}


function log_print() {
  log "$1"
  printf "> $1"
}

function send_email() {

  okaytomail=0
  # FIXME; This sucks.  Need the multiple if condition to work :\
  [ "$mail_only_errors" -ne 1 ] && okaytomail=1
  if [ "$mail_only_errors" -eq 1 ]; then
    if [ ! -z "$3" ] && [ "$3" == "err" ]; then
      okaytomail=1
    fi
  fi
  #if [ "$mail_only_errors" -ne 1 ] || [ [ "$mail_only_errors" -eq 1 ] && [ ! -z "$3" ] && [ "$3" == "err" ] ]; then
  if [ "$okaytomail" -eq 1 ]; then
    [ ! -z "$2" ] && subject="$2" || subject=""

    if [ ! -z "$mail_to" ]; then
      message="$1\n\n--\nsysbackup running on $(hostname)\n"
      printf "$message" | mail -s "$(hostname) backup report ${subject}" $mail_to
    fi
  fi
}

# Add message to e-mail
function add_email() {
  email_msg="${email_msg}$(date '+%Y-%m-%d %H:%M') ${1}\n"
}

function human_readable() {
  echo "$1"|awk '{ sum=$1 ; hum[1024**2]="GB";hum[1024**1]="MB";hum[1024]="KB"; for (x=1024**3; x>=1024; x/=1024){ if (sum>=x) { printf "%.2f %s\n",sum/x,hum[x];break } }}'
}

function is_host_up() {
  ping -c 3 -q $1 &>/dev/null
  return $?
}

function verify_bkpath() {
  if [ $backup_method -eq 0 ]; then
    [ -d "$bk_path" ] && return 0 || return 1
  else
    [ $verbose -eq 1 ] && log_print "ssh $ssh_args $bk_user@$bk_host \"[ -d ${bk_path} ] && exit 0 || exit 1\"\n"
    ssh $ssh_args $bk_user@$bk_host "[ -d $bk_path ] && exit 0 || exit 1"
  fi
  return $?
}

function check_if_same_fs() {
  #### This isn't used right now ####
  # It would be cool to use stat, but it varies between BSD and Linux and I don't want to over-complicate this
  # This is rather hackish
  # Uses df to determine if two directories are on the same filesystem
  printf "Executing: ssh $ssh_args $bk_user@$bk_host \"dirone=\"\$(df $1|tail -n 1|cut -d ' ' -f 1)\" dirtwo=\"\$(df $2|tail -n 1|cut -d ' ' -f 1)\"; [ \"\$dirone\" == \"\$dirtwo\" ] && exit 0 || exit 1\"\n"
  ssh $ssh_args $bk_user@$bk_host "dirone=\"\$(df $1|tail -n 1|cut -d ' ' -f 1)\" dirtwo=\"\$(df $2|tail -n 1|cut -d ' ' -f 1)\"; [ \"\$dirone\" == \"\$dirtwo\" ] && exit 0 || exit 1"
  return $?
}

function show_header() {
  awk 'BEGIN{$70=OFS="=";print}'
  printf "%*s\n" $(((${#1}+70)/2)) "$1"
  printf "%*s\n" $(((${#2}+70)/2)) "$2"
  awk 'BEGIN{$70=OFS="=";print}'
}

function quit() {
  if [ -e "$pid_file" ]; then
    rm -f "$pid_file"
  else
    log_print "OOPS: ${pid_file} is missing!\n"
  fi

  exit $1
}

function my_trap() {
  printf "\n==> Aborted. Cleaning up..."
  [ -e "$pid_file" ] && (rm -f "$pid_file" && log_print "Okay" || log_print "Failed to remove ${pid_file}") || log_print "OOPS: ${pid_file} is missing"
  log_print "\n"
  exit 255
}


#############################################################################
today="$(date $date_fmt)"
ver="2012-05-22"
#############################################################################


#############################################################################
# Start the program
#############################################################################

# Check if we're already running
if [ -e "$pid_file" ]; then
  my_pid=$(cat "$pid_file")
  if ps $my_pid &>/dev/null; then
    log_print "$0 is already running (pid: ${my_pid})\n"
    exit 1
  else
    err="${pid_file} reports a PID of ${my_pid}, but it doesn't seem to be running.\n"
    err="$err If it's really not running, please remove ${pid_file}\n"
    log_print "$err" ; send_email "$err" "FAILURE" "err"
    exit 1
  fi
fi

printf "$$" > $pid_file

##################################
# Did you configure it?
##################################
if [ "$i_have_configured" == "0" ]; then
  log_print "FAILURE: You need to configure the program first.\n"
  quit 1
fi

##################################
# Set the rsync filter
##################################
if [[ ! -z "$rsync_filter" && -e "$rsync_filter" ]]; then
  rsync_args="$rsync_args --include-from=${rsync_filter}"
fi


show_header "sysbackup v.${ver}" "$(date '+%F @ %H:%M')"
log "Starting sysbackup..\n"

##################################
# Check if the host is up (icmp)
##################################
if [ $check_if_up -eq 1 ]; then
  log_print "Checking if host $bk_host is up... "
  if ! is_host_up $bk_host; then
    log_print "failed\n"
    err="FAILURE: $bk_host is unreachable (via ICMP)\n"
    log_print "$err" ; send_email "$err" "FAILURE" "err"
    quit 1
  else
    log_print "ok\n"
  fi
fi

##################################
# Calculate the free space
##################################
if [ "$calculate_free_space" -eq 1 ]; then

  log_print "Calculating available space, please wait...\n"
  min_free_space=$((min_free_space*1024))
  [ $backup_method -eq 0 ] && where="on this host" || where="on the remote host"

  # Need to determine if bk_path exists first
  if ! verify_bkpath $bk_path; then
    err="FAILURE: $bk_path doesn't exist ${where}, which we need to determine accurate free space.\n"
    log_print "$err" ; send_email "$err" "FAILURE" "err"
    quit 1
  fi

  if [ $backup_method -eq 0 ]; then
    free_space=$(df -kP $bk_path|tail -1|awk '{print $4}')
  else
    free_space=$(ssh $ssh_args $bk_user@$bk_host "df -kP $bk_path|tail -1|awk '{print \$4}'")
  fi

  msg=" => Free space: $free_space KB ($(human_readable $free_space))\n"
  msg="$msg => Minimum required free space: $min_free_space KB ($(human_readable $min_free_space))\n"
  log_print "$msg" ; add_email "$msg"

  if [ "$min_free_space" -ge "$free_space" ]; then
    err=" >> FAILURE: There is not enough free space ${where} to accomodate this backup\n"
    log_print "$err" ; send_email "$err" "FAILURE" "err"
    quit 1
  fi


#  printf "Calculating sizes (this will take some time)...\n"
#  [ -f "$backup_file_list" ] && rm -f "$backup_file_list"
#
#  for data in "${data_locs[@]}"; do
#    rsync -an --stats -e "ssh $ssh_args" --link-dest="${bk_path}/Latest" "$data" $bk_user@$bk_host:$bk_path/$today.calculate >> "$backup_file_list"
#  done
#
#  #backup_size=$((`awk '{sum+=2}END{print sum}' $backup_file_list`)) # in kilobytes
#  backup_size=$(($(grep "total size is" "$backup_file_list"|awk '{sum+=$4}END{print sum}')/1024))
#  #backup_free=$(ssh $ssh_args $bk_user@$bk_host "df -k $bk_path|tail -1|awk '{print \$4}'")
#
#  echo "  Total size (in KB): $backup_size"
#  echo "  Total Free (in KB): $backup_free"
#
#  echo "$backup_file_list"


fi # calculate_free_space


[ $verbose -eq 1 ] && log_print "SSH Key: ${ssh_key} Rsync Arguments: ${rsync_args}\n"

##################################
# The actual rsync process
##################################
for data in "${data_locs[@]}"; do

  if ! is_host_up $bk_host; then
    err="FAILURE: lost connectivity to $bk_host (via ICMP)\n"
    log_print "$err" ; send_email "$err" "FAILURE" "err"
    quit 1
  fi

  # This is sloppy.  If we're backing up root, name it after the host.root
  if [ "$data" == "/" ]; then
    relative="${bk_host}.root"
  else
    relative=$(basename $data)
  fi

  if [ $backup_method = 0 ]; then
    if [ -e "$bk_path/$relative" ]; then
      find $bk_path/$relative -maxdepth 1 -mtime +$max_age -exec echo 'Removing {}' \; -exec rm -rf {} \;
      msg="Removing backups older than $max_age days\n"
      log_print "$msg" ; add_email "$msg"
    else
      msg="Creating ${bk_path}/${relative}\n"
      log_print "$msg" ; add_email "$msg"
      mkdir -p "$bk_path/$relative"
    fi

    if [ $data == "/" ]; then
      echo "here it is"
      rsync_string="${bk_user}@${bk_host}:/"
    else
      rsync_string="${bk_user}@${bk_host}:${data}/"
    fi

    rsync_string="${rsync_string} ${bk_path}/$relative/${today}.inprogress"

  else
    msg="Removing backups older than $max_age\n"
    msg="${msg}Executing: ssh $ssh_args $bk_user@$bk_host \"find $bk_path/$relative -maxdepth 1 -mtime +$max_age -exec echo 'Removing {}' \; -exec rm -rf {} \;\"\n"
    [ $verbose -eq 1 ] && log_print "$msg" ; add_email "$msg"
    ssh $ssh_args $bk_user@$bk_host "[ -e \"$bk_path/$relative\" ] && find $bk_path/$relative -maxdepth 1 -mtime +$max_age -exec echo 'Removing {}' \; -exec rm -rf {} \; || mkdir -p \"$bk_path/$relative\""
    rsync_string="${data}/. ${bk_host}:${bk_path}/$relative/${today}.inprogress"
  fi

  msg="Backing up ${data}\n"
  msg="${msg}Executing: $rsync_bin $rsync_args -e \"ssh $ssh_args -l ${bk_user}\" --link-dest=\"${bk_path}/$relative/Latest\" --stats ${rsync_string}\n"

  [ $verbose -eq 1 ] && log_print "$msg" ; add_email "$msg"

  rsync_cmd="$rsync_bin $rsync_args -e \"ssh $ssh_args\" --link-dest=\"${bk_path}/$relative/Latest\" ${rsync_string}"
  eval $rsync_cmd
  #$rsync_bin $rsync_args -e "ssh $ssh_args" --link-dest="${bk_path}/$relative/Latest" ${rsync_string}

  if [ $backup_method = 0 ]; then
      msg="Moving ${today}.inprogress to ${today} and symlinking to Latest\n"
      log_print "$msg" ; add_email "$msg"
      cd $bk_path/$relative
      mv $today.inprogress $today
      rm -f Latest;ln -s $today Latest

      # Update the time so the find routine will work right
      touch $today
  else
    msg="Moving $today}.inprogress to ${today} and symlinking to Latest\n"
    msg="${msg}Executing: ssh $ssh_args $bk_user@$bk_host \"mv $bk_path/$today.inprogress $bk_path/$relative/$today;cd $bk_path/$relative;rm -f Latest;ln -s $today Latest;touch $today\"\n"

    [ $verbose -eq 1 ] && log_print "$msg" ; add_email "$msg"
    ssh $ssh_args $bk_user@$bk_host "mv $bk_path/$relative/$today.inprogress $bk_path/$relative/$today;cd $bk_path/$relative;rm -f Latest;ln -s $today Latest;touch $today"
  fi

done

msg="==> Completed\n"
log_print "$msg"
add_email "$msg"

send_email "$email_msg"


quit 0
#EOF

