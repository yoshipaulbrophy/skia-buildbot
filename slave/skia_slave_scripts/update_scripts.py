#!/usr/bin/env python
# Copyright (c) 2012 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

""" Check out the Skia buildbot scripts. """

from utils import gclient_utils
from utils import shell_utils
from build_step import BuildStep
from config_private import SKIA_SVN_BASEURL
import os
import sys


BUILD_DIR_DEPTH = 5


class UpdateScripts(BuildStep):
  def __init__(self, attempts=5, **kwargs):
    super(UpdateScripts, self).__init__(attempts=attempts, **kwargs)

  def _Run(self):
    buildbot_dir = os.path.join(*[os.pardir for _i in range(BUILD_DIR_DEPTH)])
    os.chdir(buildbot_dir)
    if os.name == 'nt':
      svn = 'svn.bat'
    else:
      svn = 'svn'

    # Sometimes the build slaves "forget" the svn server. To prevent this from
    # occurring, use "svn ls" with --trust-server-cert.
    shell_utils.Bash([svn, 'ls', SKIA_SVN_BASEURL, '--non-interactive',
                      '--trust-server-cert'])
    gclient_utils.Sync()


if '__main__' == __name__:
  sys.exit(BuildStep.RunBuildStep(UpdateScripts))
