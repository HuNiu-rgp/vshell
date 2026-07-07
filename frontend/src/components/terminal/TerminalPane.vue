<script setup lang="ts">
import { ref, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { NTabs, NTabPane, NEmpty, NTooltip, NDropdown, NButton } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import IconPlus from '~icons/lucide/plus'
import { useTerminalStore } from '../../stores/terminal'
import { useConnectionStore } from '../../stores/connection'
import { useSFTPStore } from '../../stores/sftp'
import XTerminal from './XTerminal.vue'
import EditorTab from './EditorTab.vue'

const { t } = useI18n()
const terminalStore = useTerminalStore()
const connectionStore = useConnectionStore()
const sftpStore = useSFTPStore()

const ctxTabID = ref<string | null>(null)
const ctxX = ref(0)
const ctxY = ref(0)
const draggingTabID = ref<string | null>(null)
const dragOverTabID = ref<string | null>(null)
const dragOverPosition = ref<'before' | 'after'>('before')

function getActiveTab(): string | undefined {
  return terminalStore.activeTabID ?? undefined
}

function getTabDragProps(tab: typeof terminalStore.tabs[number]) {
  return {
    draggable: true,
    class: [
      'terminal-tab-title',
      draggingTabID.value === tab.id ? 'terminal-tab-title--dragging' : '',
      dragOverTabID.value === tab.id && draggingTabID.value !== tab.id ? `terminal-tab-title--over-${dragOverPosition.value}` : '',
    ],
    onDragstart: (e: DragEvent) => {
      draggingTabID.value = tab.id
      dragOverTabID.value = null
      dragOverPosition.value = 'before'
      e.dataTransfer?.setData('text/plain', tab.id)
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move'
      }
    },
    onDragover: (e: DragEvent) => {
      if (!draggingTabID.value || draggingTabID.value === tab.id) return
      e.preventDefault()
      dragOverTabID.value = tab.id
      const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
      dragOverPosition.value = e.clientX < rect.left + rect.width / 2 ? 'before' : 'after'
      if (e.dataTransfer) {
        e.dataTransfer.dropEffect = 'move'
      }
    },
    onDragleave: () => {
      if (dragOverTabID.value === tab.id) {
        dragOverTabID.value = null
      }
    },
    onDrop: (e: DragEvent) => {
      e.preventDefault()
      const sourceID = draggingTabID.value || e.dataTransfer?.getData('text/plain')
      if (sourceID) {
        terminalStore.moveTab(sourceID, tab.id, dragOverPosition.value)
      }
      draggingTabID.value = null
      dragOverTabID.value = null
      dragOverPosition.value = 'before'
    },
    onDragend: () => {
      draggingTabID.value = null
      dragOverTabID.value = null
      dragOverPosition.value = 'before'
    },
  }
}

function renderTabTitle(tab: typeof terminalStore.tabs[number]) {
  if (tab.type !== 'editor') {
    const dotColor = tab.connected ? 'var(--color-success)' : 'var(--text-secondary)'
    return h('span', {
      ...getTabDragProps(tab),
      style: 'display:flex;align-items:center;gap:5px',
      onContextmenu: (e: MouseEvent) => onTabContextMenu(e, tab),
    }, [
      h('span', {
        style: `width:7px;height:7px;border-radius:50%;background:${dotColor};flex-shrink:0;display:inline-block`,
      }),
      tab.title,
    ])
  }

  const children: (string | ReturnType<typeof h>)[] = []
  if (tab.isRemote === true) {
    children.push(h('span', { class: 'tab-tag-remote' }, `[${t('sftp.remotePrefix')}] `))
  } else if (tab.isRemote === false) {
    children.push(h('span', { class: 'tab-tag-local' }, `[${t('sftp.localPrefix')}] `))
  }

  children.push(tab.title)

  if (tab.dirty) {
    children.push(h('span', { class: 'tab-dirty' }, ' •'))
  }

  return h(NTooltip, { delay: 0, placement: 'bottom' }, {
    trigger: () => h('span', getTabDragProps(tab), children),
    default: () => tab.tooltip || tab.title,
  })
}

async function handleClose(id: string) {
  const tab = terminalStore.tabs.find((t) => t.id === id)
  if (tab && tab.type !== 'editor') {
    await connectionStore.disconnectSession(tab.id, tab.connectionID)
    const remaining = terminalStore.tabs.filter(t => t.connectionID === tab.connectionID && t.id !== id)
    if (tab.connectionID && remaining.length === 0) {
      sftpStore.closePanel(tab.connectionID)
    }
  }
  terminalStore.removeTab(id)
}

async function handleReconnect(tab: typeof terminalStore.tabs[number]) {
  if (tab.type === 'editor' || !tab.connectionID) return
  await connectionStore.disconnectSession(tab.id, tab.connectionID)
  terminalStore.markTabDisconnected(tab.id)
  try {
    const sessionID = await connectionStore.connect(tab.connectionID)
    terminalStore.removeTab(tab.id)
    terminalStore.addTab({
      id: sessionID,
      connectionID: tab.connectionID,
      title: tab.title,
      connected: true,
    })
  } catch (e: any) {
    // Tab stays but shows as disconnected
  }
}

async function handleDisconnectSession(tab: typeof terminalStore.tabs[number]) {
  if (tab.type === 'editor' || !tab.connectionID) return
  await connectionStore.disconnectSession(tab.id, tab.connectionID)
  terminalStore.markTabDisconnected(tab.id)
}

async function handleDuplicate(tab: typeof terminalStore.tabs[number]) {
  if (tab.type === 'editor' || !tab.connectionID) return
  try {
    const sessionID = await connectionStore.connect(tab.connectionID)
    terminalStore.addTab({
      id: sessionID,
      connectionID: tab.connectionID,
      title: tab.title,
      connected: true,
    })
  } catch (e: any) {
    // Failed silently
  }
}

async function handleNewTerminal() {
  try {
    await terminalStore.openLocalTerminal()
  } catch (e: any) {
    // Keep the tab bar action quiet; startup failures are surfaced in the terminal area.
  }
}

function handleCloseOthers(id: string) {
  const tabsToClose = terminalStore.tabs.filter(t => t.id !== id)
  for (const tab of tabsToClose) {
    if (tab.type !== 'editor') {
      connectionStore.disconnectSession(tab.id, tab.connectionID)
    }
  }
  const keepTab = terminalStore.tabs.find(t => t.id === id)
  for (const tab of tabsToClose) {
    if (tab.connectionID && tab.connectionID !== keepTab?.connectionID) {
      sftpStore.closePanel(tab.connectionID)
    }
  }
  terminalStore.closeOtherTabs(id)
}

function handleCloseAll() {
  const connectionIDs = new Set<string>()
  for (const tab of terminalStore.tabs) {
    if (tab.type !== 'editor') {
      connectionStore.disconnectSession(tab.id, tab.connectionID)
      if (tab.connectionID) connectionIDs.add(tab.connectionID)
    }
  }
  for (const connID of connectionIDs) {
    sftpStore.closePanel(connID)
  }
  terminalStore.closeAllTabs()
}

function onTabContextMenu(e: MouseEvent, tab: typeof terminalStore.tabs[number]) {
  if (tab.type === 'editor') return
  e.preventDefault()
  ctxX.value = e.clientX
  ctxY.value = e.clientY
  ctxTabID.value = tab.id
}

function getContextOptions(): DropdownOption[] {
  const tab = terminalStore.tabs.find(t => t.id === ctxTabID.value)
  if (!tab) return []
  const disconnected = !tab.connected
  return [
    { label: t('tab.duplicate'), key: 'duplicate' },
    { label: t('tab.reconnect'), key: 'reconnect', disabled: !disconnected },
    { label: t('tab.disconnect'), key: 'disconnect', disabled: disconnected },
    { type: 'divider', key: 'd1' },
    { label: t('tab.close'), key: 'close' },
    { label: t('tab.closeOthers'), key: 'closeOthers' },
    { label: t('tab.closeAll'), key: 'closeAll' },
  ]
}

function handleContextSelect(action: string) {
  const tab = terminalStore.tabs.find(t => t.id === ctxTabID.value)
  if (!tab) return
  switch (action) {
    case 'reconnect':
      handleReconnect(tab)
      break
    case 'disconnect':
      handleDisconnectSession(tab)
      break
    case 'duplicate':
      handleDuplicate(tab)
      break
    case 'close':
      handleClose(tab.id)
      break
    case 'closeOthers':
      handleCloseOthers(tab.id)
      break
    case 'closeAll':
      handleCloseAll()
      break
  }
  ctxTabID.value = null
}
</script>

<template>
  <div class="flex-1 h-full flex flex-col overflow-hidden">
    <div v-if="terminalStore.tabs.length === 0" class="flex-1 flex-center">
      <NEmpty :description="t('terminal.empty')" />
    </div>
    <div v-else class="flex-1 flex flex-col overflow-hidden min-h-0 terminal-tabs">
      <NTabs
        :value="getActiveTab()"
        type="card"
        closable
        @close="handleClose"
        @update:value="(v: string | number) => { terminalStore.activeTabID = String(v) }"
        size="small"
      >
        <template #suffix>
          <NButton
            size="tiny"
            quaternary
            class="terminal-new-tab"
            title="New local terminal"
            @click="handleNewTerminal"
          >
            <IconPlus :width="14" :height="14" />
          </NButton>
        </template>
        <NTabPane
          v-for="tab in terminalStore.tabs"
          :key="tab.id"
          :name="tab.id"
          :tab="renderTabTitle(tab)"
        />
      </NTabs>

      <NDropdown
        trigger="manual"
        :show="ctxTabID !== null"
        :x="ctxX"
        :y="ctxY"
        :options="getContextOptions()"
        @select="handleContextSelect"
        @clickoutside="ctxTabID = null"
        placement="bottom-start"
      />

      <div class="flex-1 relative min-h-0">
        <div
          v-for="tab in terminalStore.tabs"
          :key="tab.id"
          class="absolute inset-0 invisible pointer-events-none"
          :class="{ '!visible !pointer-events-auto': tab.id === terminalStore.activeTabID }"
        >
          <XTerminal v-if="tab.type !== 'editor'" :sessionID="tab.id" />
          <EditorTab v-else :tab="tab" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.terminal-tabs :deep(.n-tabs) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.terminal-tabs :deep(.n-tabs-nav) {
  flex-shrink: 0;
}

.terminal-tabs :deep(.n-tabs-content) {
  display: none;
}

.terminal-new-tab {
  width: 24px;
  height: 24px;
  margin-right: 6px;
}

.terminal-tab-title {
  cursor: grab;
  user-select: none;
}

.terminal-tab-title--dragging {
  cursor: grabbing;
  opacity: 0.55;
}

.terminal-tab-title--over-before {
  box-shadow: inset 2px 0 0 var(--color-primary);
  color: var(--color-primary);
}

.terminal-tab-title--over-after {
  box-shadow: inset -2px 0 0 var(--color-primary);
  color: var(--color-primary);
}
</style>

<style>
.tab-tag-remote {
  color: var(--color-info);
  font-weight: 600;
}
.tab-tag-local {
  color: var(--color-success);
  font-weight: 600;
}
.tab-dirty {
  color: var(--color-warning);
}
</style>
