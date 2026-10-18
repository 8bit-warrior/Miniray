import { useTranslation } from 'react-i18next';
import { Button } from 'antd';
import {
  MoonFilled,
  MoonOutlined,
  SunOutlined,
  WifiOutlined,
} from '@ant-design/icons';

import { pauseAnimationsUntilLeave, useTheme } from '@/hooks/useTheme';

interface SubHeaderProps {
  title: string;
  sId: string;
  email: string;
}

export default function SubHeader({ title, sId, email }: SubHeaderProps) {
  const { t } = useTranslation();
  const { isDark, isUltra, toggleTheme, toggleUltra } = useTheme();

  const cycleTheme = () => {
    pauseAnimationsUntilLeave('sub-theme-cycle');
    if (!isDark) {
      toggleTheme();
      if (isUltra) toggleUltra();
    } else if (!isUltra) {
      toggleUltra();
    } else {
      toggleUltra();
      toggleTheme();
    }
  };

  const themeIcon = !isDark ? <SunOutlined /> : !isUltra ? <MoonOutlined /> : <MoonFilled />;
  const initial = Array.from(title)[0]?.toUpperCase();

  return (
    <header className="sub-header">
      <div className="sub-brand">
        <span className="sub-brand-mark" aria-hidden="true">
          {initial ?? <WifiOutlined />}
        </span>
        <div className="sub-brand-text">
          <div className="sub-brand-title" dir="auto">
            {title || t('subscription.title')}
          </div>
          <div className="sub-brand-id">
            <bdi>{email ? `${sId} - ${email}` : sId}</bdi>
          </div>
        </div>
      </div>
      <div className="sub-toolbar">
        <Button
          id="sub-theme-cycle"
          shape="circle"
          size="large"
          className="toolbar-btn"
          aria-label={t('menu.theme')}
          title={t('menu.theme')}
          icon={themeIcon}
          onClick={cycleTheme}
        />
      </div>
    </header>
  );
}
