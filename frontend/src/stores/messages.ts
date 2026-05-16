import { ref } from 'vue';
import { defineStore } from 'pinia';

export type Message = {
  id: string;
  roomId: string;
  content: string;
};

export const useMessagesStore = defineStore('messages', () => {
  const messages = ref<Message[]>([]);

  function setMessages(nextMessages: Message[]) {
    messages.value = nextMessages;
  }

  function addMessage(message: Message) {
    messages.value.push(message);
  }

  return { messages, setMessages, addMessage };
});
