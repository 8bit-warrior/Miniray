import { act, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';

import AppSidebar from '@/layouts/AppSidebar';
import { renderWithProviders } from './test-utils';

vi.mock('@/api/queries/useAllSettings', () => ({
  useAllSettings: () => ({ allSetting: {} }),
}));

async function renderSidebar() {
  const view = renderWithProviders(
    <MemoryRouter>
      <AppSidebar />
    </MemoryRouter>,
  );
  await act(async () => {});
  return view;
}

test('remains expanded at 220px across remounts without a pin setting', async () => {
  const first = await renderSidebar();
  const sidebar = first.container.querySelector('.ant-layout-sider');
  const root = first.container.querySelector('.ant-sidebar');
  expect(sidebar?.classList.contains('ant-layout-sider-collapsed')).toBe(false);
  expect(root?.getAttribute('style')).toContain('--sider-rail: 220px');
  expect(screen.queryByRole('button', { name: 'Pin sidebar' })).toBeNull();

  first.unmount();
  const second = await renderSidebar();
  const restored = second.container.querySelector('.ant-layout-sider');
  expect(restored?.classList.contains('ant-layout-sider-collapsed')).toBe(false);
  expect(second.container.querySelector('.ant-sidebar')?.getAttribute('style')).toContain(
    '--sider-rail: 220px',
  );
});

test('shows the command palette shortcut for the active platform', async () => {
  const view = await renderSidebar();
  const chip = view.container.querySelector('.sidebar-command-kbd');
  expect(chip?.textContent).toBe('CtrlK');
});

test('shows settings and Xray section headers with always-visible links', async () => {
  const view = await renderSidebar();
  const groups = view.container.querySelectorAll('.sider-nav .ant-menu-item-group');
  expect(groups).toHaveLength(2);
  expect(view.container.querySelectorAll('.sider-nav .ant-menu-submenu')).toHaveLength(0);
  expect(
    view.container.querySelectorAll('.sider-nav .ant-menu-item-group-list .ant-menu-item').length,
  ).toBeGreaterThan(4);
  expect(view.container.querySelector('.brand-text')?.textContent).toBe('Miniray');
});
