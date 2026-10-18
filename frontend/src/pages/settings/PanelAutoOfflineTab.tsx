import { useEffect, useMemo, useState } from 'react';
import { Button, Input, Modal, Select, Switch, message } from 'antd';
import { PoweroffOutlined } from '@ant-design/icons';
import type { AllSetting } from '@/models/setting';
import { HttpUtil } from '@/utils';
import { SettingListItem } from '@/components/ui';

type Unit = 'minute' | 'hour' | 'day';
const MAX: Record<Unit, number> = { minute: 525600, hour: 8760, day: 365 };

export default function PanelAutoOfflineTab({
  allSetting,
  updateSetting,
  onValidityChange,
}: {
  allSetting: AllSetting;
  updateSetting: (v: Partial<AllSetting>) => void;
  onValidityChange?: (valid: boolean) => void;
}) {
  const savedText = String(allSetting.panelOfflineValue);
  const [draft, setDraft] = useState({ savedText, text: savedText });
  const text = draft.savedText === savedText ? draft.text : savedText;
  const [bootEnabled, setBootEnabled] = useState<boolean | null>(null);
  const [bootSaving, setBootSaving] = useState(false);
  const [notice, contextHolder] = message.useMessage();
  const [offlineModal, offlineModalHolder] = Modal.useModal();

  useEffect(() => {
    let active = true;
    HttpUtil.get<{ enabled: boolean }>('/panel/api/setting/panelAutostart', undefined, {
      silent: true,
    }).then((result) => {
      if (active && result?.success && result.obj) setBootEnabled(result.obj.enabled);
    });
    return () => {
      active = false;
    };
  }, []);

  const valid = useMemo(
    () =>
      /^[1-9]\d*$/.test(text) &&
      Number.isSafeInteger(Number(text)) &&
      Number(text) <= MAX[allSetting.panelOfflineUnit],
    [text, allSetting.panelOfflineUnit],
  );

  useEffect(
    () => onValidityChange?.(!allSetting.panelOfflineEnabled || valid),
    [allSetting.panelOfflineEnabled, valid, onValidityChange],
  );

  async function updateBoot(enabled: boolean) {
    setBootSaving(true);
    try {
      const result = await HttpUtil.post<{ enabled: boolean }>(
        '/panel/api/setting/panelAutostart',
        { enabled },
      );
      if (result?.success && result.obj) setBootEnabled(result.obj.enabled);
      else notice.error(result?.msg || '修改面板开机自启失败');
    } finally {
      setBootSaving(false);
    }
  }

  const panelOffline = () => {
    offlineModal.confirm({
      title: '确认让面板离线？',
      content: '面板网页与订阅服务将停止监听端口，代理内核继续运行。使用 miniray start-panel 重新开启。',
      okText: '确认离线',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        const result = await HttpUtil.post('/panel/api/setting/offlinePanel');
        if (!result?.success) throw new Error('面板离线操作失败');
      },
    });
  };

  return (
    <>
      {contextHolder}
      {offlineModalHolder}
      <SettingListItem
        title="面板离线"
        description="停止网页和订阅服务，保持独立代理内核运行。"
      >
        <Button danger icon={<PoweroffOutlined />} onClick={panelOffline}>
          面板离线
        </Button>
      </SettingListItem>
      {bootEnabled !== null && (
        <SettingListItem
          title="面板开机自启"
          description="关闭后仅禁止网页面板随系统启动；代理内核和独立协议服务的开机自启保持不变。"
        >
          <Switch
            checked={bootEnabled}
            loading={bootSaving}
            disabled={bootSaving}
            onChange={(checked) => void updateBoot(checked)}
          />
        </SettingListItem>
      )}
      <SettingListItem
        title="自动离线"
        description="每次面板启动后开始计时，到期停止网页与订阅服务；独立代理服务继续运行。"
      >
        <Switch
          checked={allSetting.panelOfflineEnabled}
          onChange={(enabled) => updateSetting({ panelOfflineEnabled: enabled })}
        />
      </SettingListItem>
      <SettingListItem title="离线倒计时" description="必须是大于零的整数，最多365天。">
        <div style={{ display: 'flex', gap: 8, width: '100%' }}>
          <Input
            value={text}
            disabled={!allSetting.panelOfflineEnabled}
            status={allSetting.panelOfflineEnabled && !valid ? 'error' : undefined}
            aria-invalid={allSetting.panelOfflineEnabled && !valid}
            onChange={(event) => {
              const value = event.target.value;
              setDraft({ savedText, text: value });
              if (
                /^[1-9]\d*$/.test(value) &&
                Number.isSafeInteger(Number(value)) &&
                Number(value) <= MAX[allSetting.panelOfflineUnit]
              ) {
                updateSetting({ panelOfflineValue: Number(value) });
              }
            }}
          />
          <Select
            style={{ minWidth: 110 }}
            value={allSetting.panelOfflineUnit}
            options={[
              { label: '分钟', value: 'minute' },
              { label: '小时', value: 'hour' },
              { label: '天', value: 'day' },
            ]}
            onChange={(unit: Unit) => updateSetting({ panelOfflineUnit: unit })}
          />
        </div>
      </SettingListItem>
    </>
  );
}
