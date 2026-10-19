import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Collapse, Modal, Progress, Radio, Spin, Tag } from 'antd';
import { CloudDownloadOutlined } from '@ant-design/icons';

import { HttpUtil, PromiseUtil } from '@/utils';
import { formatPanelVersion } from '@/lib/panel-version';
import type { PanelUpdateStatus } from '@/generated/types';
import './PanelUpdateModal.css';

type UpdateOutcome = 'success' | 'failed' | 'timeout';
type UpdatePhase = 'confirm' | 'downloading' | 'installing' | 'failed' | 'timeout';

export interface PanelUpdateInfo {
  channel?: string;
  currentVersion: string;
  latestVersion: string;
  stableLatestVersion?: string;
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

interface PanelUpdateTarget {
  version: string;
  params?: { channel: 'stable' } | { actionRunId: string };
}

interface PanelUpdateModalProps {
  open: boolean;
  info: PanelUpdateInfo;
  onClose: () => void;
}

export default function PanelUpdateModal({ open, info, onClose }: PanelUpdateModalProps) {
  const { t } = useTranslation();
  const [selectedUpdate, setSelectedUpdate] = useState<PanelUpdateTarget | null>(null);
  const [phase, setPhase] = useState<UpdatePhase>('confirm');
  const [percent, setPercent] = useState(0);
  const stableLatestVersion =
    info.stableLatestVersion || (info.channel !== 'development' ? info.latestVersion : '');
  const defaultKey = info.channel === 'development' ? 'actions' : 'stable';
  const [activeKey, setActiveKey] = useState(defaultKey);
  const [previousOpen, setPreviousOpen] = useState(open);
  const [previousChannel, setPreviousChannel] = useState(info.channel);
  if (previousOpen !== open || previousChannel !== info.channel) {
    setPreviousOpen(open);
    setPreviousChannel(info.channel);
    if (open) setActiveKey(defaultKey);
  }
  const {
    data: builds = [],
    isFetching: buildsLoading,
    isError: buildsError,
  } = useQuery({
    queryKey: ['panel', 'action-builds'],
    enabled: open && activeKey === 'actions',
    queryFn: async () => {
      const result = await HttpUtil.get<ActionBuild[]>(
        '/panel/api/server/getActionBuilds',
        undefined,
        { silent: true },
      );
      if (!result?.success) throw new Error('Unable to load development builds');
      return result.obj || [];
    },
  });

  async function pollUpdateStatus(expectedRunId: string): Promise<UpdateOutcome> {
    await PromiseUtil.sleep(5000);
    const deadline = Date.now() + 120_000;
    while (Date.now() < deadline) {
      try {
        const msg = await HttpUtil.get<PanelUpdateStatus>(
          '/panel/api/server/getUpdateStatus',
          undefined,
          { silent: true, timeout: 2000 },
        );
        const status = msg?.obj ?? undefined;
        if (status?.runId === expectedRunId) {
          if (status.state === 'installing') {
            setPhase('installing');
          } else if (status.state === 'downloading') {
            setPhase('downloading');
            if (status.progress >= 0 && status.progress < 100) setPercent(status.progress);
          }
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

  function updatePanel(target: PanelUpdateTarget) {
    setSelectedUpdate(target);
    setPhase('confirm');
    setPercent(0);
  }

  async function confirmUpdate() {
    setPhase('downloading');
    setPercent(0);
    try {
      const result = await HttpUtil.post<{ runId: string }>(
        '/panel/api/server/updatePanel',
        selectedUpdate?.params,
      );
      if (!result?.success || !result.obj?.runId) {
        setPhase('failed');
        return;
      }
      const outcome = await pollUpdateStatus(result.obj.runId);
      if (outcome === 'success') {
        await PromiseUtil.sleep(800);
        window.location.reload();
      } else {
        setPhase(outcome);
      }
    } catch {
      setPhase('failed');
    }
  }

  return (
    <>
      <Modal open={open} title={t('pages.index.updatePanel')} footer={null} onCancel={onClose}>
        <Collapse
          accordion
          activeKey={activeKey}
          onChange={(value) => setActiveKey(String(Array.isArray(value) ? value[0] || '' : value))}
          items={[
            {
              key: 'stable',
              label: t('pages.index.stableRelease'),
              children: (
                <>
                  {info.channel !== 'development' && info.updateAvailable && (
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
                      <Tag color="purple">
                        {formatPanelVersion(stableLatestVersion) || '-'}
                      </Tag>
                    </div>
                  </div>
                  <div className="actions-row">
                    <Button
                      type="primary"
                      disabled={
                        !stableLatestVersion ||
                        (info.channel !== 'development' && !info.updateAvailable)
                      }
                      onClick={() =>
                        updatePanel({
                          version: stableLatestVersion,
                          params: { channel: 'stable' },
                        })
                      }
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
                  {info.channel === 'development' && info.updateAvailable && (
                    <div className="actions-row">
                      <Button
                        type="primary"
                        onClick={() => updatePanel({ version: info.latestVersion })}
                        icon={<CloudDownloadOutlined />}
                      >
                        {t('pages.index.updatePanel')}
                      </Button>
                    </div>
                  )}
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
                              onClick={() =>
                                updatePanel({
                                  version: build.version,
                                  params: { actionRunId: build.runId },
                                })
                              }
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
      <Modal
        open={selectedUpdate !== null}
        title={t('pages.index.panelUpdateDialog')}
        closable={phase === 'confirm' || phase === 'failed' || phase === 'timeout'}
        maskClosable={phase === 'confirm'}
        keyboard={phase === 'confirm'}
        onCancel={() => setSelectedUpdate(null)}
        footer={
          phase === 'confirm'
            ? [
                <Button key="cancel" onClick={() => setSelectedUpdate(null)}>
                  {t('cancel')}
                </Button>,
                <Button key="ok" type="primary" onClick={() => void confirmUpdate()}>
                  {t('confirm')}
                </Button>,
              ]
            : phase === 'failed' || phase === 'timeout'
              ? [
                  <Button key="close" type="primary" onClick={() => setSelectedUpdate(null)}>
                    {t('confirm')}
                  </Button>,
                ]
              : null
        }
      >
        {phase === 'confirm' && (
          <p>
            {t('pages.index.panelUpdateDialogDesc').replace(
              '#version#',
              selectedUpdate?.version || '',
            )}
          </p>
        )}
        {phase === 'downloading' && (
          <div className="panel-update-download-progress">
            <p>{t('pages.index.panelUpdateDownloading')}</p>
            <Progress percent={percent} status="active" strokeLinecap="round" />
          </div>
        )}
        {phase === 'installing' && (
          <p className="panel-update-installing">{t('pages.index.panelUpdateInstalling')}</p>
        )}
        {(phase === 'failed' || phase === 'timeout') && (
          <Alert
            type={phase === 'failed' ? 'error' : 'warning'}
            showIcon
            title={t(
              phase === 'failed'
                ? 'pages.index.panelUpdateFailedTitle'
                : 'pages.index.panelUpdateUnknownTitle',
            )}
            description={t(
              phase === 'failed'
                ? 'pages.index.panelUpdateFailedDesc'
                : 'pages.index.panelUpdateUnknownDesc',
            )}
          />
        )}
      </Modal>
    </>
  );
}
