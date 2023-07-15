import { Link } from "react-router-dom";

import logo from "../images/autumn-leaf.png";
import github from "../images/github-icon.png";

const NavBar = () => {
  return (
    <nav className="flex items-center justify-between bg-purple-600 p-6 text-white w-full">
      <div className="flex items-center">
        <img src={logo} alt="Logo" className="mr-3 h-8" />
        <Link to="/" className="font-semibold text-4xl tracking-tight">
          Enviro Station
        </Link>
      </div>
      <div className="flex items-center">
        <Link to="/about" className="text-white hover:text-gray-300 text-lg">
          About
        </Link>
        <a
          href="https://github.com/wiigg/enviro-station"
          target="_blank"
          rel="noreferrer noopener"
        >
          <img src={github} alt="Github Repo" className="ml-4 h-8" />
        </a>
      </div>
    </nav>
  );
};

export default NavBar;
