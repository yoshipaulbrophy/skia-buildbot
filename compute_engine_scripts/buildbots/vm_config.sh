# This file contains constants for the shell scripts which interact with the
# Skia's GCE instances.

GCUTIL=`which gcutil`

# Set all constants in compute_engine_cfg.py as env variables.
$(python ../compute_engine_cfg.py)
if [ $? != "0" ]; then
  echo "Failed to read compute_engine_cfg.py!"
  exit 1
fi

# TODO(rmistry): Investigate moving the below constants to compute_engine_cfg.py
REQUIRED_FILES_FOR_LINUX_BOTS=(.gitconfig \
                               .netrc)
# Use a different chrome-bot password for windows due to the issue mentioned
# here: https://buganizer.corp.google.com/u/0/issues/18817374#comment29
# The password is available in valentine (win-chrome-bot).
REQUIRED_FILES_FOR_WIN_BOTS=(win-chrome-bot.txt \
                             .gitconfig \
                             .netrc)

GCOMPUTE_CMD="$GCUTIL --project=$PROJECT_ID"
GCOMPUTE_SSH_CMD="$GCOMPUTE_CMD ssh --zone=$ZONE --ssh_user=$PROJECT_USER"

GO_VERSION="go1.6.3.linux-amd64"
