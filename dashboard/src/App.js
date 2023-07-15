import { useState, useEffect } from "react";
import { Route, Routes } from "react-router-dom";
import { HubConnectionBuilder } from "@microsoft/signalr";

import About from "./components/About";
import NavBar from "./components/NavBar";
import Home from "./components/Home";
import SidePanel from "./components/SidePanel";
import InsightButton from "./components/InsightButton";
import { createNewMessage } from "./utils/data";

const App = () => {
  const [messages, setMessages] = useState([]);
  const [isPanelOpen, setIsPanelOpen] = useState(false);

  useEffect(() => {
    const baseURL = process.env.REACT_APP_REALTIME_API;
    const connection = new HubConnectionBuilder()
      .withUrl(`${baseURL}/api`)
      .withAutomaticReconnect()
      .build();

    connection.on("newMessage", (message) => {
      setMessages((messages) => {
        const newMessage = createNewMessage(message);
        const newMessages =
          messages.length >= 60 * 30
            ? [...messages.slice(1), newMessage]
            : [...messages, newMessage];
        return newMessages;
      });
    });

    connection.start().catch(console.error);

    return () => {
      connection.stop();
    };
  }, []);

  return (
    <>
      <NavBar />
      <Routes>
        <Route exact path="/" element={<Home messages={messages} />} />
        <Route path="/about" element={<About />} />
      </Routes>
      <InsightButton setIsPanelOpen={setIsPanelOpen} text={"<< Insights"} />
      <SidePanel isOpen={isPanelOpen} setIsOpen={setIsPanelOpen} />
    </>
  );
};

export default App;
