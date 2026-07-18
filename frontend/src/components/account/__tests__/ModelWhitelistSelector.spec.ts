import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('ModelWhitelistSelector', () => {
  it('can hide sync actions while retaining clear all', () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: ['claude-sonnet-4-6'],
        platform: 'anthropic',
        accountId: 1,
        showSyncActions: false
      }
    })

    expect(wrapper.text()).not.toContain('admin.accounts.fillRelatedModels')
    expect(wrapper.text()).not.toContain('admin.accounts.syncUpstreamModels')
    expect(wrapper.text()).toContain('admin.accounts.clearAllModels')
  })

  it('clear all emits an empty model list', async () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: ['claude-sonnet-4-6'],
        showSyncActions: false
      }
    })

    const clearButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.accounts.clearAllModels'))
    expect(clearButton).toBeTruthy()

    await clearButton!.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
  })

  it('includes platform-specific models when combining candidates', async () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platforms: ['antigravity', 'openai'],
        showSyncActions: false
      }
    })

    await wrapper.get('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('gpt-oss-120b-medium')
    expect(wrapper.text()).toContain('gpt-5.4')
  })

  it('shows sync actions by default for account forms', () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'anthropic'
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.fillRelatedModels')
    expect(wrapper.text()).toContain('admin.accounts.clearAllModels')
  })

  it('uses explicit available models instead of platform defaults', async () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        availableModels: ['group-only-model'],
        showSyncActions: false
      }
    })

    await wrapper.get('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('group-only-model')
    expect(wrapper.text()).not.toContain('gpt-5.4')
  })
})
