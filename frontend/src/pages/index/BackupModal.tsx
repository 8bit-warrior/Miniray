import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Checkbox, Modal } from 'antd';
import { DownloadOutlined, UploadOutlined } from '@ant-design/icons';

import { HttpUtil, PromiseUtil } from '@/utils';
import './BackupModal.css';

interface BusyEvent {
  busy: boolean;
  tip?: string;
}

interface BackupModalProps {
  open: boolean;
  basePath: string;
  onClose: () => void;
  onBusy: (e: BusyEvent) => void;
}

export default function BackupModal({
  open,
  basePath: _basePath,
  onClose,
  onBusy,
}: BackupModalProps) {
  const { t } = useTranslation();
  const [keepHostSettings, setKeepHostSettings] = useState(true);

  function exportDb() {
    window.location.href = (window.MINIRAY_BASE_PATH || '') + 'panel/api/server/getDb';
  }

  function importDb() {
    const fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.accept = '.dump,.db';
    fileInput.addEventListener('change', async (event) => {
      const file = (event.target as HTMLInputElement).files?.[0];
      if (!file) return;

      const body = new FormData();
      body.append('db', file);
      body.append('keepHostSettings', String(keepHostSettings));

      onClose();
      onBusy({ busy: true, tip: `${t('pages.index.importDatabase')}…` });
      const result = await HttpUtil.post('/panel/api/server/importDB', body, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
      if (!result?.success) {
        onBusy({ busy: false });
        return;
      }
      onBusy({ busy: true, tip: `${t('pages.settings.restartPanel')}…` });
      await PromiseUtil.sleep(5000);
      window.location.reload();
    });
    fileInput.click();
  }

  return (
    <Modal open={open} title={t('pages.index.backupTitle')} footer={null} onCancel={onClose}>
      <div className="backup-list">
        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.exportDatabase')}</div>
            <div className="backup-description">{t('pages.index.exportDatabaseDesc')}</div>
          </div>
          <Button
            type="primary"
            aria-label={t('pages.index.exportDatabase')}
            onClick={exportDb}
            icon={<DownloadOutlined />}
          />
        </div>
        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.importDatabase')}</div>
            <div className="backup-description">{t('pages.index.importDatabaseDesc')}</div>
          </div>
          <Button
            type="primary"
            aria-label={t('pages.index.importDatabase')}
            onClick={importDb}
            icon={<UploadOutlined />}
          />
        </div>
        <div className="backup-item">
          <div className="backup-meta">
            <Checkbox
              checked={keepHostSettings}
              onChange={(event) => setKeepHostSettings(event.target.checked)}
            >
              {t('pages.index.importKeepHostSettings')}
            </Checkbox>
            <div className="backup-description">{t('pages.index.importKeepHostSettingsDesc')}</div>
          </div>
        </div>
      </div>
    </Modal>
  );
}
