#!/usr/bin/env python
# Copyright (c) 2013 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

""" Download the SKPs. """

from build_step import BuildStep
from utils import gs_utils
from utils import sync_bucket_subdir
import os
import sys


class DownloadSKPs(BuildStep):
  def __init__(self, timeout=12800, no_output_timeout=9600, **kwargs):
    super (DownloadSKPs, self).__init__(
        timeout=timeout,
        no_output_timeout=no_output_timeout,
        **kwargs)

  def _CreateLocalStorageDirs(self):
    """Creates required local storage directories for this script."""
    if not os.path.exists(self._local_playback_dirs.PlaybackSkpDir()):
      os.makedirs(self._local_playback_dirs.PlaybackSkpDir())

  def _DownloadSKPsFromStorage(self):
    """Copies over skp files from Google Storage if the timestamps differ."""
    dest_gsbase = (self._args.get('dest_gsbase') or
                   sync_bucket_subdir.DEFAULT_PERFDATA_GS_BASE)
    print '\n\n========Downloading skp files from Google Storage========\n\n'
    gs_utils.download_directory_contents_if_changed(
        gs_base=dest_gsbase,
        gs_relative_dir=self._storage_playback_dirs.PlaybackSkpDir(),
        local_dir=self._local_playback_dirs.PlaybackSkpDir())

  def _Run(self):
    # Create the required local storage directories.
    self._CreateLocalStorageDirs()

    # Locally copy skps generated by webpages_playback from GoogleStorage.
    self._DownloadSKPsFromStorage()


if '__main__' == __name__:
  sys.exit(BuildStep.RunBuildStep(DownloadSKPs))
