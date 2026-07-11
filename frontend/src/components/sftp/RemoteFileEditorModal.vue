<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NModal, useDialog, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import * as monaco from 'monaco-editor'
import { SFTPWriteFileContent } from '../../../bindings/vshell/internal/app/appservice'
import { detectLanguage } from '../../utils/fileType'

const props = defineProps<{
  connectionID: string
  fileName: string
  filePath: string
  content: string
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const dialog = useDialog()
const message = useMessage()
const editorContainer = ref<HTMLElement | null>(null)
const saving = ref(false)
const dirty = ref(false)
let editor: monaco.editor.IStandaloneCodeEditor | null = null
let resizeObserver: ResizeObserver | null = null
let originalContent = props.content

async function save() {
  if (!editor || saving.value) return
  saving.value = true
  try {
    const content = editor.getValue()
    await SFTPWriteFileContent(props.connectionID, props.filePath, content)
    originalContent = content
    dirty.value = false
    message.success(t('sftp.saved'))
    emit('saved')
  } catch (error) {
    message.error(t('sftp.saveFailed', { error: error instanceof Error ? error.message : String(error) }))
  } finally {
    saving.value = false
  }
}

function close() {
  if (!dirty.value) {
    emit('close')
    return
  }
  dialog.warning({
    title: t('sftp.unsavedTitle'),
    content: t('sftp.unsavedContent'),
    positiveText: t('sftp.discard'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => emit('close'),
  })
}

onMounted(async () => {
  await nextTick()
  if (!editorContainer.value) return
  const isDark = document.documentElement.getAttribute('data-theme')?.includes('dark') !== false
  editor = monaco.editor.create(editorContainer.value, {
    value: props.content,
    language: detectLanguage(props.fileName),
    theme: isDark ? 'vs-dark' : 'vs',
    minimap: { enabled: false },
    wordWrap: 'on',
    fontSize: 13,
    lineNumbers: 'on',
    scrollBeyondLastLine: false,
    automaticLayout: false,
    padding: { top: 8 },
  })
  editor.onDidChangeModelContent(() => {
    dirty.value = editor?.getValue() !== originalContent
  })
  editor.addAction({
    id: 'save-remote-file-modal',
    label: t('common.save'),
    keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS],
    run: save,
  })
  resizeObserver = new ResizeObserver(() => editor?.layout())
  resizeObserver.observe(editorContainer.value)
  editor.focus()
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  editor?.dispose()
})
</script>

<template>
  <NModal
    :show="true"
    preset="card"
    class="remote-editor-modal"
    :title="filePath"
    :mask-closable="false"
    :close-on-esc="false"
    content-style="display: flex; flex: 1; flex-direction: column; min-height: 0; padding: 0;"
    @close="close"
  >
    <div ref="editorContainer" class="editor-surface" />
    <template #footer>
      <div class="editor-actions">
        <NButton @click="close">{{ t('common.cancel') }}</NButton>
        <NButton type="primary" :loading="saving" :disabled="!dirty" @click="save">{{ t('common.save') }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style>
.remote-editor-modal.n-card {
  display: flex;
  flex-direction: column;
  width: min(80vw, calc(100vw - 48px));
  height: min(1020px, calc(100vh - 48px));
}
.remote-editor-modal .n-card__content { flex: 1; min-height: 0; }
.editor-surface { flex: 1; min-height: 240px; }
.editor-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
