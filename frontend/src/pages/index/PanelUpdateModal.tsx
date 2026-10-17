import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Collapse, Modal, Radio, Spin, Tag } from 'antd';
import { CloudDownloadOutlined } from '@ant-design/icons';

import { HttpUtil, PromiseUtil } from '@/utils';
import { formatPanelVersion } from '@/lib/panel-version';
import type { PanelUpdateStatus } from '@/generated/types';
import './PanelUpdateModal.css';

type UpdateOutcome = 'success' | 'failed' | 'timeout';

export interface PanelUpdateInfo {
  channel?: string;
  currentVersion: string;
  latestVersion: string;
  currentCommit?: string;
  latestCommit?: string;
  updateAvailable: boolean;
}

interface ActionBuild {
  runId: string;
  commit: string;
  version: string;
  createdAt: string;
  title: string;
}

interface BusyEvent {
  busy: boolean;
  tip?: string;
}

interface PanelUpdateModalProps {
  open: boolean;
  info: PanelUpdateInfo;
  onClose: () => void;
  onBusy: (e: BusyEvent) => void;
}

export default function PanelUpdateModal({
  open,
  info,
  onClose,
  onBusy,
}: PanelUpdateModalProps) {
  const { t } = useTranslation();
  const [modal, contextHolder] = Modal.useModal();
  const [builds, setBuilds] = useState<ActionBuild[]>([]);
  const [buildsLoading, setBuildsLoading] = useState(false);
  const [buildsError, setBuildsError] = useState(false);

  async function fetchBuilds() {
    setBuildsLoading(true);
    setBuildsError(false);
    try {
      const result = await HttpUtil.get<ActionBuild[]>(
        '/panel/api/server/getActionBuilds',
        undefined,
        { silent: true },
      );
      if (!result?.success) {
        setBuildsError(true);
        return;
      }
      setBuilds(result.obj || []);
    } catch {
      setBuildsError(true);
    } finally {
      setBuildsLoading(false);
    }
  }

  async function pollUpdateStatus(expectedRunId: string): Promise<UpdateOutcome> {
    await PromiseUtil.sleep(5000);
    const deadline = Date.now() + 90_000;
    while (Date.now() < deadline) {
      try {
        const msg = await HttpUtil.get<PanelUpdateStatus>(
          '/panel/api/server/getUpdateStatus',
          undefined,
          { silent: true, timeout: 2000 },
        );
        const status = msg?.obj ?? undefined;
        if (status?.runId === expectedRunId) {
          if (status.state === 'success') return 'success';
          if (status.state === 'failed') return 'failed';
        }
      } catch {
        /* still restarting */
      }
      await PromiseUtil.sleep(2000);
    }
    return 'timeout';
  }

  function updatePanel(build?: ActionBuild) {
    modal.confirm({
      title: t('pages.index.panelUpdateDialog'),
      content: t('pages.index.panelUpdateDialogDesc').replace(
        '#version#',
        build?.version || info.latestVersion || '',
      ),
      okText: t('confirm'),
      cancelText: t('cancel'),
      onOk: async () => {
        const baseTip = t('pages.index.dontRefresh');
        const target = build?.version || info.latestVersion;
        const tip = target ? baseTip + ' (' + target + ')' : baseTip;
        onClose();
        onBusy({ busy: true, tip });
        const result = await HttpUtil.post<{ runId: string }>(
          '/panel/api/server/updatePanel',
          build ? { actionRunId: build.runId } : undefined,
        );
        if (!result?.success) {
          onBusy({ busy: false });
          return;
        }
        const outcome = await pollUpdateStatus(result.obj?.runId ?? '');
        onBusy({ busy: false });
        if (outcome === 'success') {
          await PromiseUtil.sleep(800);
          window.location.reload();
          return;
        }
        modal[outcome === 'failed' ? 'error' : 'warning']({
          title: t(
            outcome === 'failed'
              ? 'pages.index.panelUpdateFailedTitle'
              : 'pages.index.panelUpdateUnknownTitle',
          ),
          content: t(
            outcome === 'failed'
              ? 'pages.index.panelUpdateFailedDesc'
              : 'pages.index.panelUpdateUnknownDesc',
          ),
          okText: t('refresh'),
          onOk: () => window.location.reload(),
        });
      },
    });
  }

  return (
    <>
      {contextHolder}
      <Modal open={open} title={t('pages.index.updatePanel')} footer={null} onCancel={onClose}>
        <Collapse
          accordion
          defaultActiveKey="stable"
          onChange={(value) => {
            if (value === 'actions') void fetchBuilds();
          }}
          items={[
            {
              key: 'stable',
              label: t('pages.index.stableRelease'),
              children: (
                <>
                  {info.updateAvailable && (
                    <Alert
                      type="warning"
                      className="mb-12"
                      title={t('pages.index.panelUpdateDesc')}
                      showIcon
                    />
                  )}
                  <div className="version-list">
                    <div className="version-list-item">
                      <span>{t('pages.index.currentPanelVersion')}</span>
                      <Tag color="green">
                        {formatPanelVersion(window.MINIRAY_CUR_VER || info.currentVersion) || '?'}
                      </Tag>
                    </div>
                    <div className="version-list-item">
                      <span>{t('pages.index.latestPanelVersion')}</span>
                      <Tag color="purple">{formatPanelVersion(info.latestVersion) || '-'}</Tag>
                    </div>
                  </div>
                  <div className="actions-row">
                    <Button
                      type="primary"
                      disabled={!info.updateAvailable}
                      onClick={() => updatePanel()}
                      icon={<CloudDownloadOutlined />}
                    >
                      {t('pages.index.updatePanel')}
                    </Button>
                  </div>
                </>
              ),
            },
            {
              key: 'actions',
              label: t('pages.index.actionsBuilds'),
              children: (
                <>
                  <Alert
                    type="info"
                    className="mb-12"
                    title={t('pages.index.actionsBuildDesc')}
                    showIcon
                  />
                  <Spin spinning={buildsLoading}>
                    {buildsError ? (
                      <Alert type="error" title={t('pages.index.actionsBuildFailed')} showIcon />
                    ) : builds.length === 0 && !buildsLoading ? (
                      <div className="actions-build-empty">
                        {t('pages.index.actionsBuildEmpty')}
                      </div>
                    ) : (
                      <div className="version-list actions-build-list">
                        {builds.map((build) => (
                          <div className="version-list-item" key={build.runId}>
                            <div className="actions-build-info">
                              <Tag color="purple">{build.version}</Tag>
                              <span className="actions-build-meta">
                                #{build.runId} · {new Date(build.createdAt).toLocaleString()}
                              </span>
                              <span className="actions-build-title" title={build.title}>
                                {build.title}
                              </span>
                            </div>
                            <Radio
                              aria-label={t('pages.index.installBuild') + ' ' + build.version}
                              checked={
                                !!info.currentCommit &&
                                info.currentCommit.toLowerCase() === build.commit.toLowerCase()
                              }
                              onClick={() => updatePanel(build)}
                            />
                          </div>
                        ))}
                      </div>
                    )}
                  </Spin>
                </>
              ),
            },
          ]}
        />
      </Modal>
    </>
  );
}
