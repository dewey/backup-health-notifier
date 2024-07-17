# backup-health-notifier

This is a health checking script to monitor the execution of [pg_backup_rotated.sh](https://wiki.postgresql.org/wiki/Automated_Backup_on_Linux). It will send a daily email with 
the size of all backups, and a comparison to the previous day.

This does not aim to be a full monitoring solution, but rather a simple way to get notified if the backup script fails for
some reason or backups are somehow empty.

## Usage

Run as a cronjob, you can call a health checking service additionally to get notified if the job fails for some reason.

```
0 1 * * * /root/backup-health-notifier && curl --silent --output /dev/null --show-error --fail <Some https://healthchecks.io like url>
```

## Configuration

Set the following environment variables:

```
POSTMARK_TOKEN=<Postmark Api token>
POSTMARK_TEMPLATE_ID=<ID of the template on Postmark>
BACKUP_PATH=/var/lib/postgresql-backups
PASS_FILE_PATH=/root/.pgpass
CONFIG_FILE_PATH=/root/pg_backup.config
BACKUP_SCRIPT_PATH=/root/pg_backup_rotated.sh
FROM_EMAIL_ADDRESS=monitoring@example.com
TO_EMAIL_ADDRESS=mail@example.com
```


## Screenshot

Not pretty, but it does the job.

![Screenshot 2024-07-17 at 22 02 51 png@2x](https://github.com/user-attachments/assets/cf54f6c8-10e7-4cf0-9929-e945b2e30de4)

