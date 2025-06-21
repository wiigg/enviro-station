const About = () => {
  return (
    <div className="bg-gray-100 pt-2 pb-4 px-4 md:pt-4 md:px-8 border border-gray-300">
      <h1 className="text-lg mb-4 uppercase tracking-wider">About</h1>
      <p className="mb-2">
        Enviro Station stands as a comprehensive IoT-based framework designed to
        track and interpret environmental data in real-time. Created by Danny
        Wigg, a Partner Solution Architect at Microsoft, it's a contemporary
        rendition of a time-tested concept, with the application of AI to
        intelligently analyse live data.
      </p>
      <p className="mb-2">
        The solution is constructed around the Raspberry Pi Zero W, supplemented
        by Enviro and Air Quality sensors provided by{" "}
        <a
          href="https://shop.pimoroni.com/"
          target="_blank"
          rel="noreferrer noopener"
          className="text-blue-600 hover:text-blue-800 visited:text-purple-600"
        >
          Pimoroni
        </a>
        . Currently, this configuration monitors an array of environmental
        conditions in my home, including temperature, humidity, barometric
        pressure, particulate matter, and multiple gas levels.
      </p>
      <p className="mb-2">
        The inspiration behind Enviro Station hails from the pioneering efforts
        of the Central Office of Public Interest (COPI) and Imperial College
        London. Their{" "}
        <a
          href="https://www.addresspollution.org/"
          target="_blank"
          rel="noreferrer noopener"
          className="text-blue-600 hover:text-blue-800 visited:text-purple-600"
        >
          recent project
        </a>{" "}
        has been a key influence in the inception of this solution.
      </p>
      <p className="mb-2">
        For more information on the project and how it was built, you can check
        out the{" "}
        <a
          href="https://github.com/wiigg/enviro-station"
          target="_blank"
          rel="noreferrer noopener"
          className="text-blue-600 hover:text-blue-800 visited:text-purple-600"
        >
          repository
        </a>{" "}
        on GitHub.
      </p>
    </div>
  );
};

export default About;
