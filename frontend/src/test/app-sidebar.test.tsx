import { act, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';

import AppSidebar from '@/layouts/AppSidebar';
import { renderWithProviders } from './test-utils';

vi.mock('@/api/queries/useAllSettings', () => ({
  useAllSettings: () => ({ allSetting: {} }),
}));

async function renderSidebar(initialEntry = '/') {
  const view = renderWithProviders(
    <MemoryRouter initialEntries={[initialEntry]}>
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

test('does not expose command search on desktop or mobile', async () => {
  const view = await renderSidebar();
  expect(view.container.querySelector('.sidebar-command-trigger')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Open menu' }));
  expect(document.querySelector('.sidebar-command-trigger')).toBeNull();
});

test('shows direct settings and Xray links without section headings', async () => {
  const view = await renderSidebar();
  expect(view.container.querySelectorAll('.sider-nav .ant-menu-item-group')).toHaveLength(0);
  expect(view.container.querySelectorAll('.sider-nav .ant-menu-submenu')).toHaveLength(0);
  expect(view.container.querySelectorAll('.sider-nav .ant-menu-item')).toHaveLength(10);
  expect(view.container.querySelector('.brand-text')?.textContent).toBe('Miniray');
});

test.each(['/xray#advanced', '/xray#balancer', '/xray#dns'])(
  'selects advanced config in the sidebar for %s',
  async (entry) => {
    const view = await renderSidebar(entry);
    const items = Array.from(view.container.querySelectorAll('.sider-nav .ant-menu-item'));
    expect(items.at(-1)?.classList.contains('ant-menu-item-selected')).toBe(true);
    expect(items.filter((item) => item.classList.contains('ant-menu-item-selected'))).toHaveLength(
      1,
    );
  },
);
