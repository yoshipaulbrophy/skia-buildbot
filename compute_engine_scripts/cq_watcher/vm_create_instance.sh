#!/bin/bash
#
# Creates the compute instance for skia-cq-watcher.
#
set -x

source vm_config.sh

# Create a boot disk from the pushable base snapshot.
gcloud compute --project $PROJECT_ID disks create $INSTANCE_NAME \
  --zone $ZONE \
  --source-snapshot $CQ_WATCHER_SOURCE_SNAPSHOT \
  --type "pd-standard"

gcloud compute --project $PROJECT_ID instances create $INSTANCE_NAME \
  --zone $ZONE \
  --machine-type $CQ_WATCHER_MACHINE_TYPE \
  --network "default" \
  --maintenance-policy "MIGRATE" \
  --scopes $CQ_WATCHER_SCOPES \
  --tags "http-server,https-server" \
  --metadata "owner_primary=rmistry,owner_secondary=borenet" \
  --disk "name=${INSTANCE_NAME},device-name=${INSTANCE_NAME},mode=rw,boot=yes,auto-delete=yes" \
  --address $CQ_WATCHER_IP_ADDRESS

# Wait until the instance is up.
until nc -w 1 -z $CQ_WATCHER_IP_ADDRESS 22; do
    echo "Waiting for VM to come up."
    sleep 2
done

# The instance believes it is skia-systemd-snapshot-maker until it is rebooted.
echo
echo "===== Rebooting the instance ======"
# Using "shutdown -r +1" rather than "reboot" so that the connection isn't
# terminated immediately, which causes a non-zero exit code.
gcloud compute --project $PROJECT_ID ssh $PROJECT_USER@$INSTANCE_NAME \
  --zone $ZONE \
  --command "sudo shutdown -r +1" \
  || echo "Reboot failed; please reboot the instance manually."
